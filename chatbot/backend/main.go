package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	docxparser "github.com/cloudwego/eino-ext/components/document/parser/docx"
	htmlparser "github.com/cloudwego/eino-ext/components/document/parser/html"
	openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	pdfparser "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/document/parser/pdf"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/document/transformer/splitter/tfidf"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
	"github.com/sirupsen/logrus"
)

var (
	lightRAGInstance *lightrag.LightRAG
	ragGraph         compose.Runnable[string, *schema.Message]
	einoIndexer      indexer.Indexer

	// 文档解析器
	parsers     map[string]interface{}
	parsersOnce sync.Once
)

func main() {
	// 初始化 LightRAG
	if err := initLightRAG(); err != nil {
		log.Fatalf("Failed to initialize LightRAG: %v", err)
	}

	// 创建 Gin 路由
	r := gin.Default()

	// CORS 中间件
	r.Use(corsMiddleware())

	// API 路由
	api := r.Group("/api")
	{
		api.POST("/chat", handleChat)
		api.POST("/documents", handleAddDocument)
		api.POST("/upload", handleUploadDocument)
		api.GET("/documents", handleListDocuments)
		api.DELETE("/documents/:id", handleDeleteDocument)
		api.GET("/graph/full", handleGetFullGraph)
	}

	// 启动服务器
	port := os.Getenv("PORT")
	if port == "" {
		port = "45111"
	}
	log.Printf("Server starting on port %s", port)

	// 优雅关闭
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	if lightRAGInstance != nil {
		if err := lightRAGInstance.FinalizeStorages(ctx); err != nil {
			log.Printf("Failed to finalize storages: %v", err)
		}
	}

	log.Println("Server exiting")
}

func initLightRAG() error {
	ctx := context.Background()

	// 预加载 sego 词典
	if err := sego.Init(); err != nil {
		logrus.WithError(err).Warn("Failed to initialize sego dictionary")
	}

	// 设置日志级别
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// 获取环境变量
	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	openaiModel := os.Getenv("OPENAI_MODEL")
	if openaiModel == "" {
		openaiModel = "gpt-4o-mini"
	}

	openaiBaseURL := os.Getenv("OPENAI_BASE_URL")
	if openaiBaseURL == "" {
		openaiBaseURL = "https://api.openai.com/v1"
	}

	// 创建工作目录
	workingDir := os.Getenv("RAG_WORKING_DIR")
	if workingDir == "" {
		workingDir = "./rag_storage"
	}
	if err := os.MkdirAll(workingDir, 0755); err != nil {
		return fmt.Errorf("failed to create working directory: %w", err)
	}

	// 初始化 Embedder
	embedderConfig := &openaiembedding.EmbeddingConfig{
		APIKey:  openaiAPIKey,
		BaseURL: openaiBaseURL,
		Model:   "text-embedding-v4",
	}
	embedderInstance, err := lightrag.NewOpenAIEmbedder(ctx, embedderConfig)
	if err != nil {
		return fmt.Errorf("failed to create embedder: %w", err)
	}

	// 初始化 LLM
	llmConfig := &lightrag.OpenAIConfig{
		APIKey:  openaiAPIKey,
		BaseURL: openaiBaseURL,
		Model:   openaiModel,
	}
	llmInstance := lightrag.NewOpenAILLM(llmConfig)

	// 创建 LightRAG 实例
	lightRAGInstance = lightrag.New(lightrag.Options{
		WorkingDir: workingDir,
		Embedder:   embedderInstance,
		LLM:        llmInstance,
	})

	// 初始化存储
	if err := lightRAGInstance.InitializeStorages(ctx); err != nil {
		return fmt.Errorf("failed to initialize storages: %w", err)
	}

	// 初始化 Eino 组件
	cm, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		APIKey:  openaiAPIKey,
		BaseURL: openaiBaseURL,
		Model:   openaiModel,
	})
	if err != nil {
		return fmt.Errorf("failed to create eino chat model: %w", err)
	}

	// 创建 TFIDF Splitter
	splitter, err := tfidf.NewTFIDFSplitter(ctx, &tfidf.Config{
		SimilarityThreshold: 0.2,
		MaxChunkSize:        800,  // 从 10 句子改为 800 字符
		MinChunkSize:        600,  // 增加最小字符限制
		UseSego:             true, // 使用 sego 进行中文分词
		IDGenerator: func(ctx context.Context, originalID string, splitIndex int) string {
			return fmt.Sprintf("%s_chunk_%d", originalID, splitIndex)
		},
		FilterGarbageChunks: true, // 启用乱码过滤
	})
	if err != nil {
		return fmt.Errorf("failed to create TFIDF splitter: %w", err)
	}

	retrieverInstance := &LightRAGRetriever{rag: lightRAGInstance}
	einoIndexer = &LightRAGIndexer{
		rag:      lightRAGInstance,
		splitter: splitter,
	}

	// 构建 RAG Graph
	chatTemplate := prompt.FromMessages(
		schema.FString,
		schema.SystemMessage("你是一个专业的知识库助手。请根据提供的背景信息回答问题。\n\n"+
			"要求：\n"+
			"1. 回答内容必须严格基于背景信息。\n"+
			"2. 在引用背景信息的内容处，必须在行内使用 [n] 格式标注引用来源（例如 [1], [2]）。\n"+
			"3. 如果背景信息中没有相关内容，请说明你不知道。\n\n"+
			"背景信息：\n{format_docs}"),
		schema.UserMessage("{input}"),
	)

	formatDocs := func(ctx context.Context, docs []*schema.Document) (map[string]any, error) {
		mode, _ := ctx.Value("rag_mode").(lightrag.QueryMode)

		if len(docs) == 0 {
			logrus.Warn("No documents retrieved for context")
			if mode == lightrag.ModeGraph {
				return map[string]any{"format_docs": "在知识图谱中未找到相关实体或关系。"}, nil
			}
			return map[string]any{"format_docs": "未找到相关知识内容。"}, nil
		}

		uniqueTriples := make(map[string]bool)
		var graphLines []string

		// 1. 提取并去重知识图谱三元组
		for _, doc := range docs {
			if triples, ok := doc.MetaData["recalled_triples"].([]lightrag.Relationship); ok {
				for _, t := range triples {
					key := fmt.Sprintf("%s-%s-%s", t.Source, t.Relation, t.Target)
					if !uniqueTriples[key] {
						uniqueTriples[key] = true
						graphLines = append(graphLines, fmt.Sprintf("%s -[%s]-> %s", t.Source, t.Relation, t.Target))
					}
				}
			}
		}

		var contextText string
		if len(graphLines) > 0 {
			// 如果召回了图谱，优先使用图谱作为上下文
			contextText += "### 知识图谱召回信息 (Knowledge Graph):\n"
			for i, line := range graphLines {
				contextText += fmt.Sprintf("[%d] %s\n", i+1, line)
			}
			logrus.WithField("triple_count", len(graphLines)).Info("Using Knowledge Graph as primary context")

			// 在 graph 模式下，我们也提供关联的文档摘要，但强调是以图谱为主
			if mode == lightrag.ModeGraph {
				contextText += "\n### 关联文档参考 (Reference Documents):\n"
				for i, doc := range docs {
					contextText += fmt.Sprintf("[%d] %s\n", i+1, doc.Content)
				}
			}
		} else {
			// 如果没有召回图谱
			if mode == lightrag.ModeGraph {
				return map[string]any{"format_docs": "知识图谱中没有找到直接相关的三元组。"}, nil
			}

			contextText += "### 相关参考文档 (Reference Documents):\n"
			for i, doc := range docs {
				score, _ := doc.MetaData["score"].(float64)
				contextText += fmt.Sprintf("[%d] (Score: %.4f) %s\n", i+1, score, doc.Content)
			}
			logrus.Warn("No graph data found, falling back to document content")
		}

		return map[string]any{"format_docs": contextText}, nil
	}

	chain, err := compose.NewChain[string, *schema.Message]().
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, input string) (map[string]any, error) {
			// 1. 检索文档
			docs, err := retrieverInstance.Retrieve(ctx, input)
			if err != nil {
				return nil, err
			}

			// 2. 格式化文档
			formatted, err := formatDocs(ctx, docs)
			if err != nil {
				return nil, err
			}

			// 3. 将原始输入放入 map
			formatted["input"] = input
			return formatted, nil
		})).
		AppendChatTemplate(chatTemplate).
		AppendChatModel(cm).
		Compile(ctx)
	if err != nil {
		return fmt.Errorf("failed to compile eino graph: %w", err)
	}

	ragGraph = chain

	log.Println("LightRAG and Eino initialized successfully")
	return nil
}

func Ptr[T any](v T) *T {
	return &v
}

// Eino Retriever 包装
type LightRAGRetriever struct {
	rag *lightrag.LightRAG
}

func (r *LightRAGRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	options := retriever.GetCommonOptions(&retriever.Options{
		TopK: Ptr(5),
	}, opts...)

	mode, _ := ctx.Value("rag_mode").(lightrag.QueryMode)
	if mode == "" {
		mode = lightrag.ModeGlobal
	}

	logrus.WithFields(logrus.Fields{
		"query": query,
		"top_k": *options.TopK,
		"mode":  mode,
	}).Info("Retrieving documents from LightRAG")

	param := lightrag.QueryParam{
		Mode:  mode,
		Limit: *options.TopK,
	}

	results, err := r.rag.Retrieve(ctx, query, param)
	if err != nil {
		logrus.WithError(err).Error("LightRAG retrieval failed")
		return nil, err
	}

	logrus.WithField("count", len(results)).Info("Retrieved documents from LightRAG")

	docs := make([]*schema.Document, 0, len(results))
	for i, res := range results {
		logrus.WithFields(logrus.Fields{
			"index": i + 1,
			"id":    res.ID,
			"score": res.Score,
			"content_preview": func() string {
				if len(res.Content) > 200 {
					return res.Content[:200] + "..."
				}
				return res.Content
			}(),
			"recalled_triples_count": len(res.RecalledTriples),
		}).Info("LightRAG Recalled Document")

		if res.Metadata == nil {
			res.Metadata = make(map[string]any)
		}
		res.Metadata["score"] = res.Score
		if len(res.RecalledTriples) > 0 {
			res.Metadata["recalled_triples"] = res.RecalledTriples
		}

		docs = append(docs, &schema.Document{
			ID:       res.ID,
			Content:  res.Content,
			MetaData: res.Metadata,
		})
	}
	return docs, nil
}

func (r *LightRAGRetriever) GetType() string {
	return "lightrag"
}

func (r *LightRAGRetriever) IsCallbacksEnabled() bool {
	return false
}

// Eino Indexer 包装
type LightRAGIndexer struct {
	rag      *lightrag.LightRAG
	splitter document.Transformer
}

func (i *LightRAGIndexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]string, error) {
	logrus.WithField("count", len(docs)).Info("Indexing documents into LightRAG")

	// 使用 TFIDF Splitter 分割文档
	var transformedDocs []*schema.Document
	var err error
	if i.splitter != nil {
		transformedDocs, err = i.splitter.Transform(ctx, docs)
		if err != nil {
			logrus.WithError(err).Error("Failed to transform documents with TFIDF splitter")
			return nil, fmt.Errorf("failed to transform documents: %w", err)
		}
		logrus.WithFields(logrus.Fields{
			"original_count": len(docs),
			"split_count":    len(transformedDocs),
		}).Info("Documents split using TFIDF splitter")
	} else {
		transformedDocs = docs
	}

	// 过滤掉空内容的文档
	toStore := make([]map[string]any, 0, len(transformedDocs))
	skippedEmpty := 0
	for _, doc := range transformedDocs {
		// 跳过空内容或只包含空白字符的文档
		content := strings.TrimSpace(doc.Content)
		if content == "" {
			skippedEmpty++
			logrus.WithFields(logrus.Fields{
				"doc_id": doc.ID,
				"reason": "empty_content",
			}).Debug("Skipping document with empty content")
			continue
		}

		m := map[string]any{
			"id":      doc.ID,
			"content": content, // 使用清理后的内容
		}
		for k, v := range doc.MetaData {
			m[k] = v
		}
		toStore = append(toStore, m)
	}

	if skippedEmpty > 0 {
		logrus.WithField("skipped_empty_count", skippedEmpty).Info("Filtered out documents with empty content")
	}

	if len(toStore) == 0 {
		logrus.Warn("No valid documents to index after filtering")
		return []string{}, nil
	}

	logrus.WithFields(logrus.Fields{
		"total_docs":    len(toStore),
		"skipped_empty": skippedEmpty,
	}).Info("Starting batch insertion with embedding generation")

	// InsertBatch 会同步执行 embedding（在 BulkUpsert 中）
	ids, err := i.rag.InsertBatch(ctx, toStore)
	if err != nil {
		logrus.WithError(err).Error("LightRAG indexing failed")
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"indexed_count": len(ids),
		"total_docs":    len(toStore),
	}).Info("Indexed documents into LightRAG successfully with embeddings")
	return ids, nil
}

func (i *LightRAGIndexer) GetType() string {
	return "lightrag"
}

func (i *LightRAGIndexer) IsCallbacksEnabled() bool {
	return false
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

type ChatRequest struct {
	Message string             `json:"message"`
	History []string           `json:"history,omitempty"`
	Mode    lightrag.QueryMode `json:"mode,omitempty"`
}

type ChatResponse struct {
	Response string `json:"response"`
}

func handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	mode := req.Mode
	if mode == "" {
		mode = lightrag.ModeGlobal
	}

	// 使用 Eino Graph 进行查询
	logrus.WithFields(logrus.Fields{
		"message": req.Message,
		"mode":    mode,
	}).Info("Starting chat query via Eino Graph (streaming)")

	sr, err := ragGraph.Stream(context.WithValue(ctx, "rag_mode", mode), req.Message)
	if err != nil {
		logrus.WithError(err).Error("Chat query failed")
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to query via Eino: %v", err)})
		return
	}
	defer sr.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // 禁用 Nginx 缓存

	c.Stream(func(w io.Writer) bool {
		chunk, err := sr.Recv()
		if err == io.EOF {
			return false
		}
		if err != nil {
			logrus.WithError(err).Error("Stream receive failed")
			return false
		}

		if chunk != nil {
			c.SSEvent("message", chunk.Content)
			c.Writer.Flush()
		}
		return true
	})

	logrus.Info("Chat query completed successfully")
}

type AddDocumentRequest struct {
	Content string `json:"content"`
}

type AddDocumentResponse struct {
	ID string `json:"id"`
}

func handleAddDocument(c *gin.Context) {
	var req AddDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// 使用 Eino Indexer 插入文档
	_, err := einoIndexer.Store(ctx, []*schema.Document{
		{
			Content: req.Content,
		},
	})
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to insert document via Eino: %v", err)})
		return
	}

	c.JSON(200, AddDocumentResponse{
		ID: "success",
	})
}

// initParsers 初始化文档解析器
func initParsers(ctx context.Context) error {
	parsersOnce.Do(func() {
		parsers = make(map[string]interface{})

		// 初始化 PDF 解析器
		pdfParser, err := pdfparser.NewPDFParser(ctx, &pdfparser.Config{
			ToPages: true, // 按页面分割文档
		})
		if err != nil {
			logrus.WithError(err).Warn("Failed to initialize PDF parser")
		} else {
			parsers[".pdf"] = pdfParser
		}

		// 初始化 DOCX 解析器
		docxParser, err := docxparser.NewDocxParser(ctx, &docxparser.Config{
			ToSections:      true,
			IncludeComments: true,
			IncludeHeaders:  true,
			IncludeFooters:  true,
			IncludeTables:   true,
		})
		if err != nil {
			logrus.WithError(err).Warn("Failed to initialize DOCX parser")
		} else {
			parsers[".docx"] = docxParser
		}

		// 注意：XLSX 解析器暂时不可用，XLSX 文件将作为普通文本处理
		// TODO: 添加 XLSX 解析器支持

		// 初始化 HTML 解析器
		htmlParser, err := htmlparser.NewParser(ctx, &htmlparser.Config{
			Selector: nil, // 默认提取 body 内容
		})
		if err != nil {
			logrus.WithError(err).Warn("Failed to initialize HTML parser")
		} else {
			parsers[".html"] = htmlParser
			parsers[".htm"] = htmlParser
		}

		logrus.WithField("parsers", len(parsers)).Info("Document parsers initialized")
	})
	return nil
}

func handleUploadDocument(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "No file uploaded"})
		return
	}

	// 为上传操作创建带更长超时的 context（10分钟，足够处理大文件和 embedding）
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	// 初始化解析器
	if err := initParsers(ctx); err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to initialize parsers: %v", err)})
		return
	}

	// 打开文件
	f, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to open file"})
		return
	}
	defer f.Close()

	// 根据文件扩展名判断文件类型
	ext := strings.ToLower(filepath.Ext(file.Filename))

	// 尝试使用 eino-ext 解析器解析文档
	var docs []*schema.Document

	if parser, ok := parsers[ext]; ok {
		// 根据文件类型调用对应的解析器
		switch ext {
		case ".pdf":
			if pdfParser, ok := parser.(*pdfparser.PDFParser); ok {
				// PDF 解析器的 Parse 方法签名: Parse(ctx context.Context, reader io.Reader, opts ...parser.Option)
				docs, err = pdfParser.Parse(ctx, f)
			} else {
				err = fmt.Errorf("PDF parser type assertion failed")
			}
		case ".docx":
			if docxParser, ok := parser.(*docxparser.DocxParser); ok {
				// DOCX 解析器的 Parse 方法签名: Parse(ctx context.Context, reader io.Reader, opts ...parser.Option)
				docs, err = docxParser.Parse(ctx, f)
			} else {
				err = fmt.Errorf("DOCX parser type assertion failed")
			}
		case ".html", ".htm":
			if htmlParser, ok := parser.(*htmlparser.Parser); ok {
				// HTML 解析器的 Parse 方法签名: Parse(ctx context.Context, reader io.Reader, opts ...parser.Option)
				docs, err = htmlParser.Parse(ctx, f)
			} else {
				err = fmt.Errorf("HTML parser type assertion failed")
			}
		default:
			err = fmt.Errorf("unsupported parser type for extension: %s", ext)
		}

		if err != nil {
			logrus.WithError(err).WithField("extension", ext).Error("Failed to parse document")
			c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to parse document: %v", err)})
			return
		}

		logrus.WithFields(logrus.Fields{
			"filename":  file.Filename,
			"extension": ext,
			"doc_count": len(docs),
		}).Info("Successfully parsed document")
	} else {
		// 对于文本文件或其他未支持的格式，直接读取内容
		content, readErr := io.ReadAll(f)
		if readErr != nil {
			c.JSON(500, gin.H{"error": "Failed to read file"})
			return
		}

		textContent := string(content)
		if strings.TrimSpace(textContent) == "" {
			c.JSON(400, gin.H{"error": "No text content extracted from file"})
			return
		}

		docs = []*schema.Document{
			{
				Content: textContent,
				MetaData: map[string]any{
					"filename": file.Filename,
					"filetype": ext,
				},
			},
		}
		logrus.WithField("filename", file.Filename).Info("Treated as plain text file")
	}

	if len(docs) == 0 {
		c.JSON(400, gin.H{"error": "No content extracted from file"})
		return
	}

	// 为每个文档添加文件名元数据
	for _, doc := range docs {
		if doc.MetaData == nil {
			doc.MetaData = make(map[string]any)
		}
		doc.MetaData["filename"] = file.Filename
		doc.MetaData["filetype"] = ext
	}

	// 使用 Eino Indexer 插入文档（包含 embedding 操作）
	logrus.WithFields(logrus.Fields{
		"filename":  file.Filename,
		"filetype":  ext,
		"doc_count": len(docs),
	}).Info("Starting document indexing with embedding")

	ids, err := einoIndexer.Store(ctx, docs)
	if err != nil {
		logrus.WithError(err).Error("Failed to index documents")
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to insert document via Eino: %v", err)})
		return
	}

	// 检查 context 是否被取消（可能表示超时）
	contextErr := ctx.Err()
	var warning string
	if contextErr == context.DeadlineExceeded {
		warning = "Upload timeout: Some embeddings may not have completed. Please check logs for details."
		logrus.WithError(contextErr).Warn("Upload context deadline exceeded")
	} else if contextErr == context.Canceled {
		warning = "Upload canceled: Some embeddings may not have completed. Please check logs for details."
		logrus.WithError(contextErr).Warn("Upload context canceled")
	}

	logrus.WithFields(logrus.Fields{
		"filename":    file.Filename,
		"filetype":    ext,
		"doc_count":   len(docs),
		"indexed_ids": len(ids),
		"context_err": contextErr,
	}).Info("Documents indexed with embeddings")

	response := gin.H{
		"message":       "File uploaded and indexed successfully",
		"filename":      file.Filename,
		"filetype":      ext,
		"doc_count":     len(docs),
		"indexed_count": len(ids),
	}
	if warning != "" {
		response["warning"] = warning
	}
	c.JSON(200, response)
}

func handleListDocuments(c *gin.Context) {
	ctx := c.Request.Context()

	docs, err := lightRAGInstance.ListDocuments(ctx, 100, 0)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to list documents: %v", err)})
		return
	}

	c.JSON(200, gin.H{"documents": docs})
}

func handleDeleteDocument(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(400, gin.H{"error": "ID is required"})
		return
	}

	ctx := c.Request.Context()

	if err := lightRAGInstance.DeleteDocument(ctx, id); err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to delete document: %v", err)})
		return
	}

	c.JSON(200, gin.H{"message": "Document deleted successfully"})
}

func handleGetFullGraph(c *gin.Context) {
	ctx := c.Request.Context()
	docID := c.Query("doc_id")

	graph, err := lightRAGInstance.ExportGraph(ctx, docID)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to export graph: %v", err)})
		return
	}

	c.JSON(200, graph)
}
