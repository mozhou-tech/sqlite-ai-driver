# ImageRAG

ImageRAG 是一个基于 DuckDB 的图片和文本 RAG（检索增强生成）系统。

## 功能特性

1. **底层基于duckdb-driver** - 使用 DuckDB 作为存储后端，支持高效的向量搜索和全文搜索
2. **图片、文本embedding存储** - 支持图片和文本的向量化存储和检索
3. **存储图片OCR文本结果** - 自动提取图片中的文本并存储，支持基于OCR文本的搜索
4. **参考lightrag的实现** - 借鉴 LightRAG 的架构设计，提供类似的接口和功能

## 主要组件

- **ImageRAG**: 主结构，管理图片和文本的存储与检索
- **OCR**: OCR接口，用于从图片中提取文本（需要实现具体的OCR库）
- **Embedding**: 支持图片和文本的向量化
- **Search**: 支持向量搜索和全文搜索

## 使用示例

```go
import (
    "context"
    openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
    lightrag "github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag"
    "github.com/mozhou-tech/sqlite-ai-driver/pkg/imagerag"
)

// 创建 ImageRAG 实例
embedder, _ := lightrag.NewOpenAIEmbedder(ctx, &openaiembedding.EmbeddingConfig{
    APIKey:  apiKey,
    BaseURL: baseURL,
    Model:   "text-embedding-v4",
})

ocr := imagerag.NewSimpleOCR() // 注意：需要实现真实的OCR

rag := imagerag.New(imagerag.Options{
    WorkingDir: "./imagerag_storage",
    Embedder:   embedder,
    OCR:        ocr,
})

// 初始化存储
rag.InitializeStorages(ctx)

// 插入图片
rag.InsertImage(ctx, "path/to/image.jpg", metadata)

// 插入文本
rag.InsertText(ctx, "文本内容", metadata)

// 搜索
results, _ := rag.Search(ctx, "查询文本", 10)
```

## 注意事项

- OCR功能需要实现具体的OCR库（如Tesseract、云OCR服务等）
- 当前提供的 `SimpleOCR` 只是一个占位符实现，实际使用时需要替换为真实的OCR实现
- 需要运行 `go mod tidy` 来初始化依赖