package lightrag

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
)

// OpenAIEmbedder OpenAI 嵌入生成器
type OpenAIEmbedder struct {
	embedder   *openai.Embedder
	dimensions int
}

func NewOpenAIEmbedder(ctx context.Context, config *openai.EmbeddingConfig) (*OpenAIEmbedder, error) {
	emb, err := openai.NewEmbedder(ctx, config)
	if err != nil {
		return nil, err
	}

	// 默认维度，根据模型决定
	dims := 1536 // text-embedding-3-small / text-embedding-ada-002
	if config.Model == "text-embedding-3-large" {
		dims = 3072
	}

	// 如果配置中指定了维度，则优先使用
	if config.Dimensions != nil {
		dims = *config.Dimensions
	}

	return &OpenAIEmbedder{
		embedder:   emb,
		dimensions: dims,
	}, nil
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	res, err := e.embedder.EmbedStrings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return res[0], nil
}

func (e *OpenAIEmbedder) Dimensions() int {
	return e.dimensions
}

// SimpleEmbedder 简单的嵌入生成器（保留作为回退或测试用）
type SimpleEmbedder struct {
	dimensions int
}

func NewSimpleEmbedder(dims int) *SimpleEmbedder {
	if dims <= 0 {
		dims = 768
	}
	return &SimpleEmbedder{dimensions: dims}
}

func (e *SimpleEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	vec := make([]float64, e.dimensions)
	// 极简实现：取前 N 个字符的 ASCII 值
	for i := 0; i < len(text) && i < e.dimensions; i++ {
		vec[i] = float64(text[i]) / 255.0
	}
	return vec, nil
}

func (e *SimpleEmbedder) Dimensions() int {
	return e.dimensions
}
