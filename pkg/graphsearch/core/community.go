package graphsearch

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// CommunitySummarizationOptions 社区摘要选项
type CommunitySummarizationOptions struct {
	// LLMGenerator 用于生成摘要的 LLM（必需）
	LLMGenerator LLMGenerator
	// Granularity 摘要粒度
	Granularity SummarizationGranularity
	// MaxEntities 每个社区的最大实体数
	MaxEntities int
	// MaxRelations 每个社区的最大关系数
	MaxRelations int
	// IncludeMetadata 是否包含元数据
	IncludeMetadata bool
}

// SummarizationGranularity 摘要粒度
type SummarizationGranularity string

const (
	// GranularityEntity 实体级摘要
	GranularityEntity SummarizationGranularity = "entity"
	// GranularityCommunity 社区级摘要
	GranularityCommunity SummarizationGranularity = "community"
	// GranularityGlobal 全局级摘要
	GranularityGlobal SummarizationGranularity = "global"
)

// DefaultCommunitySummarizationOptions 返回默认的社区摘要选项
func DefaultCommunitySummarizationOptions(llm LLMGenerator) *CommunitySummarizationOptions {
	return &CommunitySummarizationOptions{
		LLMGenerator:    llm,
		Granularity:     GranularityCommunity,
		MaxEntities:     50,
		MaxRelations:    100,
		IncludeMetadata: true,
	}
}

// CommunitySummary 社区摘要
type CommunitySummary struct {
	// CommunityID 社区ID（实体名称或社区标识）
	CommunityID string
	// Summary 摘要文本
	Summary string
	// Entities 社区中的实体列表
	Entities []string
	// Relations 社区中的关系列表
	Relations []CommunityRelation
	// Metadata 元数据
	Metadata map[string]any
	// Granularity 摘要粒度
	Granularity SummarizationGranularity
}

// CommunityRelation 社区关系
type CommunityRelation struct {
	Source   string
	Target   string
	Relation string
}

// SummarizeCommunity 生成社区摘要
func (g *graphsearch) SummarizeCommunity(ctx context.Context, entityID string, options *CommunitySummarizationOptions) (*CommunitySummary, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if options == nil || options.LLMGenerator == nil {
		return nil, fmt.Errorf("LLMGenerator is required for community summarization")
	}

	// 获取实体的社区（子图）
	maxDepth := 2
	if options.MaxEntities > 0 {
		// 根据最大实体数估算深度
		maxDepth = int(math.Ceil(math.Sqrt(float64(options.MaxEntities))))
	}

	triples := g.getSubgraphTriples(ctx, entityID, maxDepth)

	// 收集社区中的实体和关系
	entitySet := make(map[string]bool)
	entitySet[entityID] = true
	relations := make([]CommunityRelation, 0)

	for _, triple := range triples {
		entitySet[triple.Subject] = true
		entitySet[triple.Object] = true
		relations = append(relations, CommunityRelation{
			Source:   triple.Subject,
			Target:   triple.Object,
			Relation: triple.Predicate,
		})
	}

	// 限制实体和关系数量
	entities := make([]string, 0, len(entitySet))
	for e := range entitySet {
		entities = append(entities, e)
	}
	sort.Strings(entities)

	if options.MaxEntities > 0 && len(entities) > options.MaxEntities {
		entities = entities[:options.MaxEntities]
	}

	if options.MaxRelations > 0 && len(relations) > options.MaxRelations {
		relations = relations[:options.MaxRelations]
	}

	// 根据粒度生成摘要
	var summary string
	var err error

	switch options.Granularity {
	case GranularityEntity:
		summary, err = g.generateEntitySummary(ctx, entityID, entities, relations, options)
	case GranularityCommunity:
		summary, err = g.generateCommunitySummary(ctx, entityID, entities, relations, options)
	case GranularityGlobal:
		summary, err = g.generateGlobalSummary(ctx, entities, relations, options)
	default:
		summary, err = g.generateCommunitySummary(ctx, entityID, entities, relations, options)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	metadata := make(map[string]any)
	if options.IncludeMetadata {
		metadata["entity_count"] = len(entities)
		metadata["relation_count"] = len(relations)
		metadata["root_entity"] = entityID
	}

	return &CommunitySummary{
		CommunityID: entityID,
		Summary:     summary,
		Entities:    entities,
		Relations:   relations,
		Metadata:    metadata,
		Granularity: options.Granularity,
	}, nil
}

// SummarizeCommunities 批量生成社区摘要
func (g *graphsearch) SummarizeCommunities(ctx context.Context, entityIDs []string, options *CommunitySummarizationOptions) ([]*CommunitySummary, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	summaries := make([]*CommunitySummary, 0, len(entityIDs))

	for _, entityID := range entityIDs {
		summary, err := g.SummarizeCommunity(ctx, entityID, options)
		if err != nil {
			// 记录错误但继续处理其他实体
			continue
		}
		summaries = append(summaries, summary)
	}

	return summaries, nil
}

// generateEntitySummary 生成实体级摘要
func (g *graphsearch) generateEntitySummary(ctx context.Context, entityID string, entities []string, relations []CommunityRelation, options *CommunitySummarizationOptions) (string, error) {
	// 构建提示词
	prompt := fmt.Sprintf(`请为以下实体生成摘要。

实体名称：%s

相关实体：
%s

相关关系：
%s

请生成一个简洁的实体摘要，描述该实体的主要特征和关系。`, entityID, formatEntities(entities), formatRelations(relations))

	llmOptions := map[string]any{
		"temperature": 0.3,
		"max_tokens":  500,
	}

	summary, err := options.LLMGenerator.Generate(ctx, prompt, llmOptions)
	if err != nil {
		return "", fmt.Errorf("LLM generation failed: %w", err)
	}

	return summary, nil
}

// generateCommunitySummary 生成社区级摘要
func (g *graphsearch) generateCommunitySummary(ctx context.Context, rootEntity string, entities []string, relations []CommunityRelation, options *CommunitySummarizationOptions) (string, error) {
	// 构建提示词
	prompt := fmt.Sprintf(`请为以下实体社区生成摘要。

根实体：%s

社区中的实体（共 %d 个）：
%s

社区中的关系（共 %d 个）：
%s

请生成一个社区摘要，描述该社区的整体特征、主要实体和它们之间的关系。`, rootEntity, len(entities), formatEntities(entities), len(relations), formatRelations(relations))

	llmOptions := map[string]any{
		"temperature": 0.3,
		"max_tokens":  1000,
	}

	summary, err := options.LLMGenerator.Generate(ctx, prompt, llmOptions)
	if err != nil {
		return "", fmt.Errorf("LLM generation failed: %w", err)
	}

	return summary, nil
}

// generateGlobalSummary 生成全局级摘要
func (g *graphsearch) generateGlobalSummary(ctx context.Context, entities []string, relations []CommunityRelation, options *CommunitySummarizationOptions) (string, error) {
	// 构建提示词
	prompt := fmt.Sprintf(`请为以下知识图谱生成全局摘要。

实体总数：%d
关系总数：%d

主要实体：
%s

主要关系：
%s

请生成一个全局摘要，描述整个知识图谱的结构、主要主题和关键关系。`, len(entities), len(relations), formatEntities(entities), formatRelations(relations))

	llmOptions := map[string]any{
		"temperature": 0.3,
		"max_tokens":  1500,
	}

	summary, err := options.LLMGenerator.Generate(ctx, prompt, llmOptions)
	if err != nil {
		return "", fmt.Errorf("LLM generation failed: %w", err)
	}

	return summary, nil
}

// formatEntities 格式化实体列表
func formatEntities(entities []string) string {
	if len(entities) == 0 {
		return "无"
	}

	result := ""
	for i, entity := range entities {
		if i > 0 {
			result += "\n"
		}
		result += fmt.Sprintf("- %s", entity)
		if i >= 20 { // 限制显示数量
			result += fmt.Sprintf("\n... (共 %d 个实体)", len(entities))
			break
		}
	}
	return result
}

// formatRelations 格式化关系列表
func formatRelations(relations []CommunityRelation) string {
	if len(relations) == 0 {
		return "无"
	}

	result := ""
	for i, rel := range relations {
		if i > 0 {
			result += "\n"
		}
		result += fmt.Sprintf("- %s --[%s]--> %s", rel.Source, rel.Relation, rel.Target)
		if i >= 20 { // 限制显示数量
			result += fmt.Sprintf("\n... (共 %d 个关系)", len(relations))
			break
		}
	}
	return result
}

// DetectCommunities 检测社区（使用简单的连通分量算法）
func (g *graphsearch) DetectCommunities(ctx context.Context, maxDepth int) ([]Community, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	// 获取所有实体
	entities, err := g.ListEntities(ctx, 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}

	// 构建图
	graph := make(map[string]map[string]bool)
	visited := make(map[string]bool)

	for _, entity := range entities {
		entityID := entity["entity_id"].(string)
		graph[entityID] = make(map[string]bool)
		visited[entityID] = false

		// 获取邻居
		triples := g.getSubgraphTriples(ctx, entityID, maxDepth)
		for _, triple := range triples {
			if triple.Subject == entityID {
				graph[entityID][triple.Object] = true
			}
			if triple.Object == entityID {
				graph[entityID][triple.Subject] = true
			}
		}
	}

	// 使用 DFS 查找连通分量
	communities := make([]Community, 0)

	for entityID := range graph {
		if !visited[entityID] {
			community := Community{
				ID:       fmt.Sprintf("community_%d", len(communities)+1),
				Entities: make([]string, 0),
			}

			// DFS 遍历
			stack := []string{entityID}
			for len(stack) > 0 {
				current := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				if visited[current] {
					continue
				}
				visited[current] = true
				community.Entities = append(community.Entities, current)

				for neighbor := range graph[current] {
					if !visited[neighbor] {
						stack = append(stack, neighbor)
					}
				}
			}

			if len(community.Entities) > 0 {
				communities = append(communities, community)
			}
		}
	}

	return communities, nil
}

// Community 社区结构
type Community struct {
	ID       string
	Entities []string
}
