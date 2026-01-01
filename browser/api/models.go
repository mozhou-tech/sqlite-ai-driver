package main

import "time"

// Document 文档模型
type Document struct {
	ID             string    `gorm:"primaryKey;type:varchar(255);not null"`
	CollectionName string    `gorm:"type:varchar(255);not null;index"`
	Data           string    `gorm:"type:text"`        // JSON 格式存储
	Embedding      string    `gorm:"type:FLOAT[1024]"` // 向量数据，存储为数组
	Content        string    `gorm:"type:text"`        // 提取的文本内容，用于全文搜索
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

// TableName 指定表名
func (Document) TableName() string {
	return "documents"
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// CollectionInfo 集合信息
type CollectionInfo struct {
	Name   string                 `json:"name"`
	Schema map[string]interface{} `json:"schema"`
}

// DocumentResponse 文档响应
type DocumentResponse struct {
	ID   string                 `json:"id"`
	Data map[string]interface{} `json:"data"`
}

// FulltextSearchRequest 全文搜索请求
type FulltextSearchRequest struct {
	Collection string  `json:"collection"`
	Query      string  `json:"query"`
	Limit      int     `json:"limit"`
	Threshold  float64 `json:"threshold"`
}

// VectorSearchRequest 向量搜索请求
type VectorSearchRequest struct {
	Collection string    `json:"collection,omitempty"`
	Query      []float64 `json:"query,omitempty"`
	QueryText  string    `json:"query_text,omitempty"`
	Limit      int       `json:"limit,omitempty"`
	Field      string    `json:"field,omitempty"`
	Threshold  float64   `json:"threshold,omitempty"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error string `json:"error"`
}

// GraphLinkRequest 图链接请求
type GraphLinkRequest struct {
	From     string `json:"from" binding:"required"`
	Relation string `json:"relation" binding:"required"`
	To       string `json:"to" binding:"required"`
}

// GraphPathRequest 图路径请求
type GraphPathRequest struct {
	From      string   `json:"from" binding:"required"`
	To        string   `json:"to" binding:"required"`
	MaxDepth  int      `json:"max_depth"`
	Relations []string `json:"relations,omitempty"`
}

// GraphQueryRequest 图查询请求
type GraphQueryRequest struct {
	Query string `json:"query" binding:"required"`
}
