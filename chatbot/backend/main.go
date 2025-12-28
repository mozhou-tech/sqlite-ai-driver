package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag"
	"github.com/sirupsen/logrus"
)

var (
	lightRAGInstance *lightrag.LightRAG
	ragGraph         compose.Runnable[string, *schema.Message]
	einoIndexer      indexer.Indexer
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
	}

	// 启动服务器
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
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
		Model:   "text-embedding-3-small",
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

	retrieverInstance := &LightRAGRetriever{rag: lightRAGInstance}
	einoIndexer = &LightRAGIndexer{rag: lightRAGInstance}

	// 构建 RAG Graph
	chatTemplate := prompt.FromMessages(
		schema.FString,
		schema.SystemMessage("你是一个专业的助手。请根据以下背景信息回答问题：\n\n背景信息：\n{format_docs}"),
		schema.UserMessage("{input}"),
	)

	formatDocs := func(ctx context.Context, docs []*schema.Document) (map[string]any, error) {
		if len(docs) == 0 {
			logrus.Warn("No documents retrieved for context")
			return map[string]any{"format_docs": "未找到相关文档。"}, nil
		}
		var contextText string
		for i, doc := range docs {
			contextText += fmt.Sprintf("[%d] %s\n", i+1, doc.Content)
		}

		logrus.WithFields(logrus.Fields{
			"doc_count":      len(docs),
			"context_length": len(contextText),
		}).Info("Formatted documents for LLM context")

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

	logrus.WithFields(logrus.Fields{
		"query": query,
		"top_k": *options.TopK,
	}).Info("Retrieving documents from LightRAG")

	param := lightrag.QueryParam{
		Mode:  lightrag.ModeGlobal,
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
			"content_preview": func() string {
				if len(res.Content) > 200 {
					return res.Content[:200] + "..."
				}
				return res.Content
			}(),
		}).Debug("LightRAG Recalled Document Content")

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
	rag *lightrag.LightRAG
}

func (i *LightRAGIndexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]string, error) {
	logrus.WithField("count", len(docs)).Info("Indexing documents into LightRAG")

	toStore := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		m := map[string]any{
			"id":      doc.ID,
			"content": doc.Content,
		}
		for k, v := range doc.MetaData {
			m[k] = v
		}
		toStore = append(toStore, m)
	}

	ids, err := i.rag.InsertBatch(ctx, toStore)
	if err != nil {
		logrus.WithError(err).Error("LightRAG indexing failed")
		return nil, err
	}

	logrus.WithField("count", len(ids)).Info("Indexed documents into LightRAG successfully")
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

	sr, err := ragGraph.Stream(ctx, req.Message)
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

func handleUploadDocument(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "No file uploaded"})
		return
	}

	// 打开文件
	f, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to open file"})
		return
	}
	defer f.Close()

	// 读取内容
	content, err := io.ReadAll(f)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to read file"})
		return
	}

	ctx := c.Request.Context()

	// 使用 Eino Indexer 插入文档
	_, err = einoIndexer.Store(ctx, []*schema.Document{
		{
			Content: string(content),
			MetaData: map[string]any{
				"filename": file.Filename,
			},
		},
	})
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to insert document via Eino: %v", err)})
		return
	}

	c.JSON(200, gin.H{
		"message":  "File uploaded and indexed successfully",
		"filename": file.Filename,
	})
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
