package utils

import (
	"context"
	"fmt"
)

// DefaultGraphRAGOptions 返回默认的 GraphRAG 配置选项
func DefaultGraphRAGOptions() *GraphRAGOptions {
	return &GraphRAGOptions{
		RetrievalOptions:    DefaultRetrievalOptions(),
		OrganizationOptions: DefaultOrganizationOptions(),
		GenerationOptions:   DefaultGenerationOptions(),
	}
}

// DefaultRetrievalOptions 返回默认的检索选项
func DefaultRetrievalOptions() *RetrievalOptions {
	return &RetrievalOptions{
		Limit:               10,
		MaxDepth:            2,
		SimilarityThreshold: 0.0,
		Strategy:            StrategyHybrid,
	}
}

// DefaultOrganizationOptions 返回默认的组织选项
func DefaultOrganizationOptions() *OrganizationOptions {
	return &OrganizationOptions{
		EnablePruning:        true,
		EnableReranking:      true,
		EnableAugmentation:   false,
		EnableVerbalization:  true,
		PruningOptions:       DefaultPruningOptions(),
		RerankingOptions:     DefaultRerankingOptions(),
		VerbalizationOptions: DefaultVerbalizationOptions(),
	}
}

// DefaultGenerationOptions 返回默认的生成选项
func DefaultGenerationOptions() *GenerationOptions {
	return &GenerationOptions{
		Method:          MethodHybrid,
		MaxLength:       500,
		Temperature:     0.7,
		IncludeSources:  true,
		UseGraphContext: true,
	}
}

// DefaultPruningOptions 返回默认的剪枝选项
func DefaultPruningOptions() *PruningOptions {
	return &PruningOptions{
		MaxNodes:         50,
		MaxEdges:         100,
		MinScore:         0.3,
		KeepCoreEntities: true,
	}
}

// DefaultRerankingOptions 返回默认的重排序选项
func DefaultRerankingOptions() *RerankingOptions {
	return &RerankingOptions{
		Method:            RerankByHybrid,
		TopK:              10,
		UseGraphStructure: true,
	}
}

// DefaultVerbalizationOptions 返回默认的文本化选项
func DefaultVerbalizationOptions() *VerbalizationOptions {
	return &VerbalizationOptions{
		Format:          FormatNaturalLanguage,
		IncludeMetadata: false,
		MaxLength:       500,
	}
}

// ValidateGraphRAGOptions 验证 GraphRAG 选项的有效性
func ValidateGraphRAGOptions(options *GraphRAGOptions) error {
	if options == nil {
		return nil // 使用默认选项
	}

	if options.RetrievalOptions != nil {
		if err := ValidateRetrievalOptions(options.RetrievalOptions); err != nil {
			return fmt.Errorf("invalid retrieval options: %w", err)
		}
	}

	if options.OrganizationOptions != nil {
		if err := ValidateOrganizationOptions(options.OrganizationOptions); err != nil {
			return fmt.Errorf("invalid organization options: %w", err)
		}
	}

	if options.GenerationOptions != nil {
		if err := ValidateGenerationOptions(options.GenerationOptions); err != nil {
			return fmt.Errorf("invalid generation options: %w", err)
		}
	}

	return nil
}

// ValidateRetrievalOptions 验证检索选项
func ValidateRetrievalOptions(options *RetrievalOptions) error {
	if options == nil {
		return nil
	}

	if options.Limit < 0 {
		return fmt.Errorf("limit must be non-negative, got %d", options.Limit)
	}

	if options.MaxDepth < 0 {
		return fmt.Errorf("maxDepth must be non-negative, got %d", options.MaxDepth)
	}

	if options.SimilarityThreshold < 0 || options.SimilarityThreshold > 1 {
		return fmt.Errorf("similarityThreshold must be in [0, 1], got %f", options.SimilarityThreshold)
	}

	return nil
}

// ValidateOrganizationOptions 验证组织选项
func ValidateOrganizationOptions(options *OrganizationOptions) error {
	if options == nil {
		return nil
	}

	if options.PruningOptions != nil {
		if err := ValidatePruningOptions(options.PruningOptions); err != nil {
			return fmt.Errorf("invalid pruning options: %w", err)
		}
	}

	if options.RerankingOptions != nil {
		if err := ValidateRerankingOptions(options.RerankingOptions); err != nil {
			return fmt.Errorf("invalid reranking options: %w", err)
		}
	}

	return nil
}

// ValidatePruningOptions 验证剪枝选项
func ValidatePruningOptions(options *PruningOptions) error {
	if options == nil {
		return nil
	}

	if options.MaxNodes < 0 {
		return fmt.Errorf("maxNodes must be non-negative, got %d", options.MaxNodes)
	}

	if options.MaxEdges < 0 {
		return fmt.Errorf("maxEdges must be non-negative, got %d", options.MaxEdges)
	}

	if options.MinScore < 0 || options.MinScore > 1 {
		return fmt.Errorf("minScore must be in [0, 1], got %f", options.MinScore)
	}

	return nil
}

// ValidateRerankingOptions 验证重排序选项
func ValidateRerankingOptions(options *RerankingOptions) error {
	if options == nil {
		return nil
	}

	if options.TopK < 0 {
		return fmt.Errorf("topK must be non-negative, got %d", options.TopK)
	}

	return nil
}

// ValidateGenerationOptions 验证生成选项
func ValidateGenerationOptions(options *GenerationOptions) error {
	if options == nil {
		return nil
	}

	if options.MaxLength < 0 {
		return fmt.Errorf("maxLength must be non-negative, got %d", options.MaxLength)
	}

	if options.Temperature < 0 || options.Temperature > 2 {
		return fmt.Errorf("temperature must be in [0, 2], got %f", options.Temperature)
	}

	return nil
}

// ValidateQuery 验证查询字符串
func ValidateQuery(query string) error {
	if query == "" {
		return fmt.Errorf("query cannot be empty")
	}

	if len(query) > 10000 {
		return fmt.Errorf("query too long, max length is 10000, got %d", len(query))
	}

	return nil
}

// WithRetrievalStrategy 设置检索策略的便捷函数
func WithRetrievalStrategy(strategy RetrievalStrategy) func(*GraphRAGOptions) {
	return func(opts *GraphRAGOptions) {
		if opts.RetrievalOptions == nil {
			opts.RetrievalOptions = DefaultRetrievalOptions()
		}
		opts.RetrievalOptions.Strategy = strategy
	}
}

// WithRetrievalLimit 设置检索数量限制的便捷函数
func WithRetrievalLimit(limit int) func(*GraphRAGOptions) {
	return func(opts *GraphRAGOptions) {
		if opts.RetrievalOptions == nil {
			opts.RetrievalOptions = DefaultRetrievalOptions()
		}
		opts.RetrievalOptions.Limit = limit
	}
}

// WithRetrievalMaxDepth 设置检索最大深度的便捷函数
func WithRetrievalMaxDepth(maxDepth int) func(*GraphRAGOptions) {
	return func(opts *GraphRAGOptions) {
		if opts.RetrievalOptions == nil {
			opts.RetrievalOptions = DefaultRetrievalOptions()
		}
		opts.RetrievalOptions.MaxDepth = maxDepth
	}
}

// WithGenerationMethod 设置生成方法的便捷函数
func WithGenerationMethod(method GenerationMethod) func(*GraphRAGOptions) {
	return func(opts *GraphRAGOptions) {
		if opts.GenerationOptions == nil {
			opts.GenerationOptions = DefaultGenerationOptions()
		}
		opts.GenerationOptions.Method = method
	}
}

// WithRerankingMethod 设置重排序方法的便捷函数
func WithRerankingMethod(method RerankingMethod) func(*GraphRAGOptions) {
	return func(opts *GraphRAGOptions) {
		if opts.OrganizationOptions == nil {
			opts.OrganizationOptions = DefaultOrganizationOptions()
		}
		if opts.OrganizationOptions.RerankingOptions == nil {
			opts.OrganizationOptions.RerankingOptions = DefaultRerankingOptions()
		}
		opts.OrganizationOptions.RerankingOptions.Method = method
	}
}

// BuildGraphRAGOptions 使用便捷函数构建 GraphRAG 选项
func BuildGraphRAGOptions(funcs ...func(*GraphRAGOptions)) *GraphRAGOptions {
	opts := DefaultGraphRAGOptions()
	for _, f := range funcs {
		f(opts)
	}
	return opts
}

// GraphRAGQueryWithOptions 使用便捷函数执行 GraphRAG 查询
func (g *graphsearch) GraphRAGQueryWithOptions(ctx context.Context, query string, funcs ...func(*GraphRAGOptions)) (*GeneratedAnswer, error) {
	options := BuildGraphRAGOptions(funcs...)
	return g.GraphRAGQuery(ctx, query, options)
}
