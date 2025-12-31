package imagerag

import "context"

// Embedder 向量嵌入生成器接口
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
}

// Options ImageRAG配置选项
type Options struct {
	WorkingDir    string
	TextEmbedder  Embedder
	ImageEmbedder Embedder
	OCR           OCR
}

// SearchResult 搜索结果
type SearchResult struct {
	ID      string
	Content string
	Score   float64
	Source  string // "vector", "text", "image", "hybrid"
	Data    map[string]any
}
