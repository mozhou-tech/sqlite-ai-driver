package graphsearch

import "context"

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
