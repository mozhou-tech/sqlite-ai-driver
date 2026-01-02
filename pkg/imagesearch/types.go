package imagesearch

import "context"

// Embedder 向量嵌入生成器接口
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
}

// Options ImageSearch配置选项
type Options struct {
	WorkingDir    string // 工作目录，默认为 "./testdata"
	TextEmbedder  Embedder
	ImageEmbedder Embedder
	OCR           OCR
	TablePrefix   string // 表前缀，默认为 "imagesearch_"
}

// SearchResult 搜索结果
type SearchResult struct {
	ID      int64
	Content string
	Score   float64
	Source  string // "vector", "text", "image", "hybrid"
	Data    map[string]any
}

// MetadataFilter metadata过滤条件
// 支持按key-value进行过滤，多个条件之间为AND关系
// 例如：MetadataFilter{"source": "example", "category": "test"} 表示同时满足 source="example" 且 category="test"
type MetadataFilter map[string]any
