package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/lightragstore"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
)

// LightRAGRetriever 将 LightRAG 包装为 Eino retriever。
type LightRAGRetriever struct {
	rag *lightrag.LightRAG
}

func (r *LightRAGRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	param := lightrag.QueryParam{
		Mode:  lightrag.ModeHybrid,
		Limit: 5,
	}

	results, err := r.rag.Retrieve(ctx, query, param)
	if err != nil {
		return nil, err
	}

	docs := make([]*schema.Document, 0, len(results))
	for _, res := range results {
		if res.Metadata == nil {
			res.Metadata = make(map[string]any)
		}
		res.Metadata["score"] = res.Score

		docs = append(docs, &schema.Document{
			ID:       res.ID,
			Content:  res.Content,
			MetaData: res.Metadata,
		})
	}
	return docs, nil
}

func (r *LightRAGRetriever) GetType() string          { return "lightrag" }
func (r *LightRAGRetriever) IsCallbacksEnabled() bool { return false }

// LightRAGIndexer 将 LightRAG 包装为 Eino indexer。
type LightRAGIndexer struct {
	rag *lightrag.LightRAG
}

func (i *LightRAGIndexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]string, error) {
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

	return i.rag.InsertBatch(ctx, toStore)
}

func (i *LightRAGIndexer) GetType() string          { return "lightrag" }
func (i *LightRAGIndexer) IsCallbacksEnabled() bool { return false }

func main() {
	ctx := context.Background()

	// 1. 设置环境变量和配置
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("请设置 OPENAI_API_KEY 环境变量")
		return
	}
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}

	// 初始化 sego 词典以获得更好的中文处理效果
	if err := sego.Init(); err != nil {
		log.Printf("警告：sego 初始化失败: %v", err)
	}

	workingDir := "./testdata/toc_storage"
	_ = os.MkdirAll(workingDir, 0755)

	// 2. 初始化 LightRAG 组件
	embedder, err := lightrag.NewOpenAIEmbedder(ctx, &openaiembedding.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   "text-embedding-v4",
	})
	if err != nil {
		log.Fatalf("创建 embedder 失败: %v", err)
	}

	llm := lightrag.NewOpenAILLM(&lightrag.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})

	rag := lightrag.New(lightrag.Options{
		WorkingDir: workingDir,
		Embedder:   embedder,
		LLM:        llm,
	})

	if err := rag.InitializeStorages(ctx); err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	// 3. 检查命令行参数并索引素材
	if len(os.Args) < 2 {
		fmt.Println("用法: go run examples/eino_agent/main.go <path_to_txt_file> [topic]")
		return
	}

	txtPath := os.Args[1]
	content, err := os.ReadFile(txtPath)
	if err != nil {
		log.Fatalf("读取文件失败: %v", err)
	}

	idx := &LightRAGIndexer{rag: rag}
	_, err = idx.Store(ctx, []*schema.Document{
		{
			ID:      filepath.Base(txtPath),
			Content: string(content),
		},
	})
	if err != nil {
		log.Fatalf("索引文档失败: %v", err)
	}
	fmt.Printf("成功索引素材: %s，正在等待图谱提取完成...\n", txtPath)
	rag.Wait()
	fmt.Println("图谱提取完成。")

	// 4. 设置 Eino Agent/Chain 用于文章生成
	topic := "如何写一篇好文章"
	if len(os.Args) >= 3 {
		topic = os.Args[2]
	}

	cm, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})
	if err != nil {
		log.Fatalf("创建 chat model 失败: %v", err)
	}

	ret := &LightRAGRetriever{rag: rag}

	// 定义文章目录生成的 Prompt 模板
	template := prompt.FromMessages(
		schema.FString,
		schema.SystemMessage("你是一个专业的图书编辑和结构化思维专家。请根据提供的背景信息，为关于 '{topic}' 的文章生成一个详细且逻辑严密的目录（Table of Contents）。\n\n"+
			"目录要求：\n"+
			"1. 结构清晰，层级分明（建议使用：一、1. (1) 或类似的标准层级格式）。\n"+
			"2. 深度覆盖背景信息中的核心概念、实体及其关联关系。\n"+
			"3. 逻辑连贯，从浅入深，能够引导读者全面理解该主题。\n\n"+
			"背景信息：\n{context}"),
		schema.UserMessage("请为主题 '{topic}' 生成详细的文章目录。"),
	)

	// 使用 Eino Chain 编排 Agent 流程
	chain, err := compose.NewChain[string, string]().
		// 1. 检索并准备上下文
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, input string) (map[string]any, error) {
			// 1.1 检索文档
			docs, err := ret.Retrieve(ctx, input)
			if err != nil {
				return nil, err
			}

			// 1.2 检索并处理 Graph 结构 (三层深度)
			var graphContext string
			graphData, err := ret.rag.SearchGraphWithDepth(ctx, input, 3)
			if err == nil && graphData != nil {
				// 打印用于调试
				fmt.Println("\n--- 召回的 Graph 结构 ---")
				fmt.Printf("实体 (%d): ", len(graphData.Entities))
				for i, entity := range graphData.Entities {
					fmt.Print(entity.Name)
					if i < len(graphData.Entities)-1 {
						fmt.Print(", ")
					}
				}
				fmt.Printf("\n关系 (%d): \n", len(graphData.Relationships))
				for _, rel := range graphData.Relationships {
					fmt.Printf("  %s --[%s]--> %s\n", rel.Source, rel.Relation, rel.Target)
				}
				fmt.Println("--------------------------")

				// 格式化为注入 LLM 的上下文
				if len(graphData.Relationships) > 0 {
					graphContext = "核心知识关联（三元组）：\n"
					for _, rel := range graphData.Relationships {
						graphContext += fmt.Sprintf("- %s --(%s)--> %s\n", rel.Source, rel.Relation, rel.Target)
					}
					graphContext += "\n"
				}
			}

			var contextText string
			// 优先注入结构化的图谱信息
			if graphContext != "" {
				contextText += graphContext + "详细参考文本：\n"
			}

			for i, doc := range docs {
				contextText += fmt.Sprintf("[%d] %s\n", i+1, doc.Content)
			}

			if contextText == "" {
				contextText = "（未找到相关背景信息，请根据你的通用知识库进行创作）"
			}

			return map[string]any{
				"topic":   input,
				"context": contextText,
			}, nil
		})).
		// 2. 应用 Prompt 模板
		AppendChatTemplate(template).
		// 3. 调用 LLM
		AppendChatModel(cm).
		// 4. 解析输出
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, msg *schema.Message) (string, error) {
			return msg.Content, nil
		})).
		Compile(ctx)

	if err != nil {
		log.Fatalf("编译 Eino Chain 失败: %v", err)
	}

	// 5. 运行 Agent
	fmt.Printf("\n正在为主题 '%s' 生成文章目录...\n\n", topic)
	toc, err := chain.Invoke(ctx, topic)
	if err != nil {
		log.Fatalf("目录生成失败: %v", err)
	}

	fmt.Println("--- 生成的文章目录 ---")
	fmt.Println(toc)
	fmt.Println("----------------------")
	fmt.Println("\n目录生成任务完成。")
}
