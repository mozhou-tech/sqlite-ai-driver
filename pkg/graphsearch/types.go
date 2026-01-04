package graphsearch

import (
	"context"
	"time"
)

// Embedder 向量嵌入生成器接口
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
}

// Options graphsearch配置选项
type Options struct {
	Embedder   Embedder
	WorkingDir string // 工作目录，作为基础目录
	TableName  string // SQLite 表名，默认为 "graphsearch_entities"
}

// Entity GORM 模型，表示图谱存储中的实体
type Entity struct {
	EntityID        string    `gorm:"type:VARCHAR;primaryKey;not null;column:entity_id"`
	EntityName      string    `gorm:"type:TEXT;column:entity_name"`
	Metadata        string    `gorm:"type:TEXT;column:metadata"`  // JSON 格式的元数据
	Embedding       string    `gorm:"type:TEXT;column:embedding"` // JSON 格式的向量数组
	EmbeddingStatus string    `gorm:"type:VARCHAR;default:'pending';column:embedding_status"`
	CreatedAt       time.Time `gorm:"autoCreateTime;column:created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime;column:updated_at"`
}

// TableName 指定表名
func (Entity) TableName() string {
	return "graphsearch_entities"
}

// SemanticSearchResult 语义搜索结果
type SemanticSearchResult struct {
	EntityID   string
	EntityName string
	Score      float64
	Metadata   map[string]any
	Triples    []Triple // 与该实体相关的三元组
}

// Triple 表示图数据库中的三元组（subject-predicate-object）
type Triple struct {
	Subject   string
	Predicate string
	Object    string
}
