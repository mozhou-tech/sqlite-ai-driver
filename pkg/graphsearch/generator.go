package graphsearch

import (
	"context"
	"fmt"
	"strings"
)

// LLMGenerator LLM生成器接口（可选，用于集成外部LLM）
type LLMGenerator interface {
	Generate(ctx context.Context, prompt string, options map[string]any) (string, error)
}

// defaultGenerator 默认生成器实现
type defaultGenerator struct {
	graphsearch  *graphsearch
	llmGenerator LLMGenerator // 可选的LLM生成器
}

// NewDefaultGenerator 创建默认生成器
func NewDefaultGenerator(gs *graphsearch, llm LLMGenerator) Generator {
	return &defaultGenerator{
		graphsearch:  gs,
		llmGenerator: llm,
	}
}

// Generate 生成答案
func (g *defaultGenerator) Generate(ctx context.Context, organizedResult *OrganizedResult, query string, options *GenerationOptions) (*GeneratedAnswer, error) {
	if organizedResult == nil {
		return nil, fmt.Errorf("organized result is nil")
	}

	if options == nil {
		options = &GenerationOptions{
			Method:          MethodHybrid,
			MaxLength:       500,
			Temperature:     0.7,
			IncludeSources:  true,
			UseGraphContext: true,
		}
	}

	var answer *GeneratedAnswer
	var err error

	switch options.Method {
	case MethodDiscrimination:
		answer, err = g.generateWithDiscrimination(ctx, organizedResult, query, options)
	case MethodLLM:
		answer, err = g.GenerateWithLLM(ctx, organizedResult, query, options)
	case MethodGraph:
		answer, err = g.GenerateWithGraph(ctx, organizedResult, query, options)
	case MethodHybrid:
		// 混合方法：结合多种生成方式
		answer, err = g.generateHybrid(ctx, organizedResult, query, options)
	default:
		answer, err = g.generateWithDiscrimination(ctx, organizedResult, query, options)
	}

	if err != nil {
		return nil, err
	}

	return answer, nil
}

// GenerateWithLLM 使用LLM生成答案
func (g *defaultGenerator) GenerateWithLLM(ctx context.Context, organizedResult *OrganizedResult, query string, options *GenerationOptions) (*GeneratedAnswer, error) {
	if g.llmGenerator == nil {
		// 如果没有LLM生成器，回退到基于图的方法
		return g.GenerateWithGraph(ctx, organizedResult, query, options)
	}

	// 构建提示词
	prompt := g.buildPrompt(organizedResult, query, options)

	// 调用LLM生成
	llmOptions := map[string]any{
		"temperature": options.Temperature,
		"max_length":  options.MaxLength,
	}

	answerText, err := g.llmGenerator.Generate(ctx, prompt, llmOptions)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	// 构建答案来源
	sources := g.extractSources(organizedResult, options)

	return &GeneratedAnswer{
		Answer:     answerText,
		Confidence: 0.8, // LLM生成的置信度
		Sources:    sources,
		Metadata:   make(map[string]any),
	}, nil
}

// GenerateWithGraph 基于图结构生成答案
func (g *defaultGenerator) GenerateWithGraph(ctx context.Context, organizedResult *OrganizedResult, query string, options *GenerationOptions) (*GeneratedAnswer, error) {
	// 基于图结构生成答案：从子图和三元组中提取信息
	var answerParts []string

	// 使用文本化后的内容
	if len(organizedResult.VerbalizedTexts) > 0 {
		answerParts = append(answerParts, organizedResult.VerbalizedTexts...)
	} else {
		// 如果没有文本化，手动构建
		for _, subgraph := range organizedResult.Subgraphs {
			text := g.subgraphToText(subgraph)
			answerParts = append(answerParts, text)
		}
	}

	// 组合答案
	answerText := strings.Join(answerParts, "\n\n")

	// 限制长度
	if options.MaxLength > 0 && len(answerText) > options.MaxLength {
		answerText = answerText[:options.MaxLength] + "..."
	}

	// 构建答案来源
	sources := g.extractSources(organizedResult, options)

	return &GeneratedAnswer{
		Answer:     answerText,
		Confidence: 0.7, // 基于图的生成置信度
		Sources:    sources,
		Metadata:   make(map[string]any),
	}, nil
}

// generateWithDiscrimination 基于判别的方法生成答案
func (g *defaultGenerator) generateWithDiscrimination(ctx context.Context, organizedResult *OrganizedResult, query string, options *GenerationOptions) (*GeneratedAnswer, error) {
	// 基于判别的方法：从检索结果中选择最相关的片段组合成答案
	var answerParts []string

	// 按相关性排序实体
	sortedEntities := make([]RetrievedEntity, len(organizedResult.Entities))
	copy(sortedEntities, organizedResult.Entities)

	// 简单排序（实际应该使用更复杂的判别模型）
	for i := 0; i < len(sortedEntities)-1; i++ {
		for j := i + 1; j < len(sortedEntities); j++ {
			if sortedEntities[i].Score < sortedEntities[j].Score {
				sortedEntities[i], sortedEntities[j] = sortedEntities[j], sortedEntities[i]
			}
		}
	}

	// 选择TopK实体构建答案
	topK := 3
	if len(sortedEntities) < topK {
		topK = len(sortedEntities)
	}

	for i := 0; i < topK; i++ {
		entity := sortedEntities[i]
		text := fmt.Sprintf("关于 %s：", entity.EntityName)

		// 添加相关三元组信息
		if len(entity.Triples) > 0 {
			tripleTexts := make([]string, 0, len(entity.Triples))
			for _, triple := range entity.Triples {
				tripleTexts = append(tripleTexts, fmt.Sprintf("%s %s %s", triple.Subject, triple.Predicate, triple.Object))
			}
			text += "\n" + strings.Join(tripleTexts, "\n")
		}

		answerParts = append(answerParts, text)
	}

	answerText := strings.Join(answerParts, "\n\n")

	// 限制长度
	if options.MaxLength > 0 && len(answerText) > options.MaxLength {
		answerText = answerText[:options.MaxLength] + "..."
	}

	// 构建答案来源
	sources := g.extractSources(organizedResult, options)

	return &GeneratedAnswer{
		Answer:     answerText,
		Confidence: 0.75, // 基于判别的生成置信度
		Sources:    sources,
		Metadata:   make(map[string]any),
	}, nil
}

// generateHybrid 混合生成方法
func (g *defaultGenerator) generateHybrid(ctx context.Context, organizedResult *OrganizedResult, query string, options *GenerationOptions) (*GeneratedAnswer, error) {
	// 结合多种方法
	var answers []*GeneratedAnswer

	// 1. 基于判别的方法
	discAnswer, err := g.generateWithDiscrimination(ctx, organizedResult, query, options)
	if err == nil {
		answers = append(answers, discAnswer)
	}

	// 2. 基于图的方法
	graphAnswer, err := g.GenerateWithGraph(ctx, organizedResult, query, options)
	if err == nil {
		answers = append(answers, graphAnswer)
	}

	// 3. 如果有LLM，使用LLM方法
	if g.llmGenerator != nil {
		llmAnswer, err := g.GenerateWithLLM(ctx, organizedResult, query, options)
		if err == nil {
			answers = append(answers, llmAnswer)
		}
	}

	if len(answers) == 0 {
		return nil, fmt.Errorf("all generation methods failed")
	}

	// 合并答案（取最长的或综合多个答案）
	bestAnswer := answers[0]
	maxLength := len(bestAnswer.Answer)

	for _, ans := range answers[1:] {
		if len(ans.Answer) > maxLength {
			maxLength = len(ans.Answer)
			bestAnswer = ans
		}
	}

	// 合并所有来源
	allSources := make([]Source, 0)
	for _, ans := range answers {
		allSources = append(allSources, ans.Sources...)
	}
	bestAnswer.Sources = allSources

	// 计算平均置信度
	totalConfidence := 0.0
	for _, ans := range answers {
		totalConfidence += ans.Confidence
	}
	bestAnswer.Confidence = totalConfidence / float64(len(answers))

	return bestAnswer, nil
}

// buildPrompt 构建LLM提示词
func (g *defaultGenerator) buildPrompt(organizedResult *OrganizedResult, query string, options *GenerationOptions) string {
	var parts []string

	parts = append(parts, "基于以下图谱信息回答问题：")
	parts = append(parts, fmt.Sprintf("问题：%s", query))
	parts = append(parts, "\n图谱信息：")

	// 添加文本化后的内容
	if len(organizedResult.VerbalizedTexts) > 0 {
		parts = append(parts, strings.Join(organizedResult.VerbalizedTexts, "\n\n"))
	} else {
		// 手动构建
		for _, subgraph := range organizedResult.Subgraphs {
			parts = append(parts, g.subgraphToText(subgraph))
		}
	}

	parts = append(parts, "\n请基于以上信息回答问题。")

	return strings.Join(parts, "\n")
}

// subgraphToText 将子图转换为文本
func (g *defaultGenerator) subgraphToText(subgraph Subgraph) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("实体：%s", subgraph.RootEntity))

	if len(subgraph.Triples) > 0 {
		parts = append(parts, "关系：")
		for _, triple := range subgraph.Triples {
			parts = append(parts, fmt.Sprintf("  - %s %s %s", triple.Subject, triple.Predicate, triple.Object))
		}
	}

	return strings.Join(parts, "\n")
}

// extractSources 提取答案来源
func (g *defaultGenerator) extractSources(organizedResult *OrganizedResult, options *GenerationOptions) []Source {
	if !options.IncludeSources {
		return []Source{}
	}

	sources := make([]Source, 0)

	// 从实体中提取来源
	for _, entity := range organizedResult.Entities {
		sources = append(sources, Source{
			Type:     "entity",
			ID:       entity.EntityID,
			Score:    entity.Score,
			Metadata: entity.Metadata,
		})
	}

	// 从子图中提取来源
	for i, subgraph := range organizedResult.Subgraphs {
		sources = append(sources, Source{
			Type:     "subgraph",
			ID:       fmt.Sprintf("subgraph_%d", i),
			Score:    subgraph.Score,
			Metadata: subgraph.Metadata,
		})
	}

	return sources
}
