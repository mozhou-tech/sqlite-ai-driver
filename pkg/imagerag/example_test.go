package imagerag_test

import (
	"context"
	"fmt"
	"log"
	"os"

	openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/imagerag"
)

// OpenAIEmbedderWrapper 包装OpenAI embedder以符合imagerag.Embedder接口
type OpenAIEmbedderWrapper struct {
	embedder *openaiembedding.Embedder
	dims     int
}

func NewOpenAIEmbedderWrapper(ctx context.Context, config *openaiembedding.EmbeddingConfig) (*OpenAIEmbedderWrapper, error) {
	emb, err := openaiembedding.NewEmbedder(ctx, config)
	if err != nil {
		return nil, err
	}

	dims := 1536 // 默认维度
	if config.Model == "text-embedding-3-large" {
		dims = 3072
	}
	if config.Dimensions != nil {
		dims = *config.Dimensions
	}

	return &OpenAIEmbedderWrapper{
		embedder: emb,
		dims:     dims,
	}, nil
}

func (e *OpenAIEmbedderWrapper) Embed(ctx context.Context, text string) ([]float64, error) {
	res, err := e.embedder.EmbedStrings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return res[0], nil
}

func (e *OpenAIEmbedderWrapper) Dimensions() int {
	return e.dims
}

// ExampleImageRAG 展示如何使用ImageRAG
func ExampleImageRAG() {
	ctx := context.Background()

	// 1. 设置环境变量和配置
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Println("请设置 OPENAI_API_KEY 环境变量")
		return
	}
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// 2. 初始化 Embedder
	// 可以使用同一个embedder作为textEmbedder和imageEmbedder
	// 或者使用不同的embedder（例如，图片使用视觉模型，文本使用文本模型）
	textEmbedder, err := NewOpenAIEmbedderWrapper(ctx, &openaiembedding.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   "text-embedding-v4",
	})
	if err != nil {
		log.Fatalf("创建 text embedder 失败: %v", err)
	}

	// 图片embedder可以使用相同的或不同的模型
	imageEmbedder, err := NewOpenAIEmbedderWrapper(ctx, &openaiembedding.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   "text-embedding-v4", // 可以使用不同的模型
	})
	if err != nil {
		log.Fatalf("创建 image embedder 失败: %v", err)
	}

	// 3. 初始化 OCR（这里使用SimpleOCR作为示例，实际应该使用真实的OCR实现）
	ocr := imagerag.NewSimpleOCR()

	// 4. 创建 ImageRAG 实例
	rag := imagerag.New(imagerag.Options{
		WorkingDir:    "./imagerag_storage",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	// 5. 初始化存储
	if err := rag.InitializeStorages(ctx); err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	defer rag.Close(ctx)

	// 6. 插入图片
	imagePath := "./test_image.jpg"
	if _, err := os.Stat(imagePath); err == nil {
		metadata := map[string]any{
			"source": "example",
			"tags":   []string{"test", "example"},
		}
		if err := rag.InsertImage(ctx, imagePath, metadata); err != nil {
			log.Printf("插入图片失败: %v", err)
		} else {
			fmt.Println("图片插入成功")
		}
	}

	// 7. 插入文本
	text := "这是一段测试文本，用于演示文本插入功能。"
	if err := rag.InsertText(ctx, text, nil); err != nil {
		log.Printf("插入文本失败: %v", err)
	} else {
		fmt.Println("文本插入成功")
	}

	// 8. 执行搜索
	results, err := rag.Search(ctx, "测试", 10)
	if err != nil {
		log.Printf("搜索失败: %v", err)
	} else {
		fmt.Printf("找到 %d 个结果\n", len(results))
		for i, result := range results {
			fmt.Printf("结果 %d: %s (分数: %.4f, 来源: %s)\n", i+1, result.Content, result.Score, result.Source)
		}
	}
}
