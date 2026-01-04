package graphsearch

import (
	"context"
)

// SimpleEmbedder 简单的嵌入生成器实现（用于测试）
type SimpleEmbedder struct {
	dimensions int
}

// NewSimpleEmbedder 创建简单的嵌入生成器
func NewSimpleEmbedder(dimensions int) Embedder {
	return &SimpleEmbedder{dimensions: dimensions}
}

func (e *SimpleEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	// 简单的实现：返回固定维度的零向量
	embedding := make([]float64, e.dimensions)
	return embedding, nil
}

func (e *SimpleEmbedder) Dimensions() int {
	return e.dimensions
}
