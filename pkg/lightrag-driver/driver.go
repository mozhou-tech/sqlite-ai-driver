package lightrag_driver

import (
	"context"
)

// Database 定义数据库接口
type Database interface {
	// Collection 获取或创建集合
	Collection(ctx context.Context, name string, schema Schema) (Collection, error)
	// Graph 获取图数据库实例
	Graph() GraphDatabase
	// Close 关闭数据库连接
	Close(ctx context.Context) error
}

// Schema 定义集合的schema
type Schema struct {
	PrimaryKey string
	RevField   string
}

// Collection 定义文档集合接口
type Collection interface {
	// Insert 插入文档
	Insert(ctx context.Context, doc map[string]any) (Document, error)
	// FindByID 根据ID查找文档
	FindByID(ctx context.Context, id string) (Document, error)
	// Find 根据选项查找文档
	Find(ctx context.Context, opts FindOptions) ([]Document, error)
	// Delete 根据ID删除文档
	Delete(ctx context.Context, id string) error
	// BulkUpsert 批量插入或更新文档
	BulkUpsert(ctx context.Context, docs []map[string]any) ([]Document, error)
}

// FindOptions 查找选项
type FindOptions struct {
	Limit    int
	Offset   int
	Selector map[string]any
}

// Document 定义文档接口
type Document interface {
	// ID 返回文档ID
	ID() string
	// Data 返回文档数据
	Data() map[string]any
}

// FulltextSearch 定义全文搜索接口
type FulltextSearch interface {
	// FindWithScores 执行全文搜索并返回带分数的结果
	FindWithScores(ctx context.Context, query string, opts FulltextSearchOptions) ([]FulltextSearchResult, error)
	// Close 关闭全文搜索资源
	Close() error
}

// FulltextSearchOptions 全文搜索选项
type FulltextSearchOptions struct {
	Limit    int
	Selector map[string]any
}

// FulltextSearchResult 全文搜索结果
type FulltextSearchResult struct {
	Document Document
	Score    float64
}

// VectorSearch 定义向量搜索接口
type VectorSearch interface {
	// Search 执行向量搜索
	Search(ctx context.Context, embedding []float64, opts VectorSearchOptions) ([]VectorSearchResult, error)
	// Close 关闭向量搜索资源
	Close() error
}

// VectorSearchOptions 向量搜索选项
type VectorSearchOptions struct {
	Limit    int
	Selector map[string]any
}

// VectorSearchResult 向量搜索结果
type VectorSearchResult struct {
	Document Document
	Score    float64
}

// GraphDatabase 定义图数据库接口
type GraphDatabase interface {
	// Link 创建一条从 subject 到 object 的边，边的类型为 predicate
	Link(ctx context.Context, subject, predicate, object string) error
	// GetNeighbors 获取指定节点的邻居节点
	GetNeighbors(ctx context.Context, node, predicate string) ([]string, error)
	// Query 返回查询构建器
	Query() GraphQuery
}

// GraphQuery 定义图查询构建器接口
type GraphQuery interface {
	// V 选择指定的节点
	V(node string) GraphQuery
	// Both 获取双向邻居
	Both() GraphQuery
	// All 执行查询并返回所有结果
	All(ctx context.Context) ([]GraphQueryResult, error)
}

// GraphQueryResult 图查询结果
type GraphQueryResult struct {
	Subject   string
	Predicate string
	Object    string
}

// DatabaseOptions 数据库选项
type DatabaseOptions struct {
	Name         string
	Path         string
	GraphOptions *GraphOptions
}

// GraphOptions 图数据库选项
type GraphOptions struct {
	Enabled bool
	Backend string
}

// FulltextSearchConfig 全文搜索配置
type FulltextSearchConfig struct {
	Identifier  string
	DocToString func(doc map[string]any) string
}

// VectorSearchConfig 向量搜索配置
type VectorSearchConfig struct {
	Identifier     string
	DocToEmbedding func(doc map[string]any) ([]float64, error)
	Dimensions     int
}
