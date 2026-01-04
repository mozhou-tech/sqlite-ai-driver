package imagesearch

import (
	"context"
	"time"
)

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

// Document GORM 模型，表示图片和文本存储中的文档
type Document struct {
	ID              []byte    `gorm:"type:BLOB(16);primaryKey;not null"` // UUID 二进制
	Content         string    `gorm:"type:TEXT"`                         // JSON 格式的文档内容
	Metadata        string    `gorm:"type:TEXT"`                         // JSON 格式的元数据
	TextEmbedding   string    `gorm:"type:TEXT"`                         // 文本向量（JSON 数组格式）
	ImageEmbedding  string    `gorm:"type:TEXT"`                         // 图片向量（JSON 数组格式）
	EmbeddingStatus string    `gorm:"type:TEXT;default:'pending'"`       // embedding 状态
	Rev             int       `gorm:"column:_rev;default:1"`             // 版本号
	CreatedAt       time.Time `gorm:"autoCreateTime"`                    // 创建时间
}

// TableName 动态表名，由 Collection 设置
func (Document) TableName() string {
	// 这个方法会被 Collection 覆盖，这里只是占位符
	return "imagesearch_documents"
}

// SearchResult 搜索结果
type SearchResult struct {
	ID      string
	Content string
	Score   float64
	Source  string // "vector", "text", "image", "hybrid"
	Data    map[string]any
}

// MetadataFilter metadata过滤条件
// 支持按key-value进行过滤，多个条件之间为AND关系
// 例如：MetadataFilter{"source": "example", "category": "test"} 表示同时满足 source="example" 且 category="test"
type MetadataFilter map[string]any
