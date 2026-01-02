# ImageSearch

ImageSearch 是一个基于 SQLite 的图片和文本 RAG（检索增强生成）系统。

## 功能特性

1. **底层基于sqlite-driver** - 直接使用 SQLite 作为存储后端，支持高效的向量搜索
2. **图片、文本embedding存储** - 支持图片和文本的向量化存储和检索
3. **存储图片OCR文本结果** - 自动提取图片中的文本并存储，用于生成embedding
4. **向量检索** - 仅使用向量相似度搜索，不依赖全文搜索
5. **独立实现** - 不依赖 lightrag，直接使用 sqlite-driver 实现所有功能

## 主要组件

- **ImageSearch**: 主结构，管理图片和文本的存储与检索
- **OCR**: OCR接口，用于从图片中提取文本（需要实现具体的OCR库）
- **Embedding**: 支持图片和文本的向量化
- **VectorSearch**: 基于向量相似度的检索

## 使用示例

```go
import (
    "context"
    openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
    "github.com/mozhou-tech/sqlite-ai-driver/pkg/imagesearch"
)

// 实现 Embedder 接口（可以使用任何embedding库）
type MyEmbedder struct {
    // ... 实现 Embedder 接口
}

// 创建 ImageSearch 实例
// 可以分别设置文本和图片的embedder，或使用同一个embedder
textEmbedder := &MyEmbedder{} // 用于文本embedding
imageEmbedder := &MyEmbedder{} // 用于图片embedding（基于OCR文本）
ocr := imagesearch.NewSimpleOCR() // 注意：需要实现真实的OCR

rag := imagesearch.New(imagesearch.Options{
    WorkingDir:    "./testdata", // 可选，默认为 "./testdata"
    TextEmbedder:  textEmbedder,
    ImageEmbedder: imageEmbedder,
    OCR:           ocr,
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

- **OCR功能**：需要实现具体的OCR库（如Tesseract、云OCR服务等）。当前提供的 `SimpleOCR` 只是一个占位符实现
- **Embedder接口**：需要实现 `Embedder` 接口，可以使用任何embedding库（如OpenAI、DashScope等）。支持分别设置 `TextEmbedder` 和 `ImageEmbedder`，可以针对文本和图片使用不同的embedding模型。**必须提供至少一个Embedder才能使用搜索功能**
- **仅向量检索**：只支持向量相似度搜索，不支持全文搜索
- **依赖管理**：需要运行 `go mod tidy` 来初始化依赖
- **独立实现**：已完全移除对 lightrag 的依赖，直接使用 sqlite-driver 实现所有功能