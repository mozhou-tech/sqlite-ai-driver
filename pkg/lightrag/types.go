package lightrag

import (
	"context"
)

// QueryMode 查询模式
type QueryMode string

const (
	ModeHybrid   QueryMode = "hybrid"   // 混合搜索（向量 + 全文）
	ModeVector   QueryMode = "vector"   // 向量搜索
	ModeFulltext QueryMode = "fulltext" // 全文搜索
	ModeGraph    QueryMode = "graph"    // 图搜索
	ModeLocal    QueryMode = "local"    // 局部搜索（基于实体）
	ModeGlobal   QueryMode = "global"   // 全局搜索（基于关系/社区）
)

// QueryParam 查询参数
type QueryParam struct {
	Mode      QueryMode      `json:"mode"`
	Limit     int            `json:"limit"`
	Threshold float64        `json:"threshold"` // 分数阈值
	Filters   map[string]any `json:"filters"`   // 元数据过滤器 (Mango Selector)
}

// SearchResult 搜索结果
type SearchResult struct {
	ID       string                 `json:"id"`
	Content  string                 `json:"content"`
	Score    float64                `json:"score"`
	Source   string                 `json:"source"`
	Metadata map[string]interface{} `json:"metadata"`
}

// Entity 实体
type Entity struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Relationship 关系
type Relationship struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	Relation    string `json:"relation"`
	Description string `json:"description"`
}

// GraphData 知识图谱数据
type GraphData struct {
	Entities      []Entity       `json:"entities"`
	Relationships []Relationship `json:"relationships"`
}

// Embedder 向量嵌入生成器接口（复用或参考 cognee.Embedder）
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
}

// LLM 语言模型接口
type LLM interface {
	Complete(ctx context.Context, prompt string) (string, error)
}
