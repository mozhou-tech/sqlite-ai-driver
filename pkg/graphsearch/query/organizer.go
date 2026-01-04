package graphsearch

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

// defaultOrganizer 默认组织器实现
type defaultOrganizer struct {
	graphsearch *graphsearch
}

// NewDefaultOrganizer 创建默认组织器
func NewDefaultOrganizer(gs *graphsearch) Organizer {
	return &defaultOrganizer{
		graphsearch: gs,
	}
}

// Organize 组织检索结果
func (o *defaultOrganizer) Organize(ctx context.Context, result *RetrievalResult, options *OrganizationOptions) (*OrganizedResult, error) {
	if result == nil {
		return nil, fmt.Errorf("retrieval result is nil")
	}

	if options == nil {
		options = &OrganizationOptions{
			EnablePruning:       true,
			EnableReranking:     true,
			EnableAugmentation:  false,
			EnableVerbalization: false,
		}
	}

	organized := &OrganizedResult{
		Entities:  result.Entities,
		Triples:   result.Triples,
		Subgraphs: result.Subgraphs,
		Metadata:  result.Metadata,
	}

	// 图剪枝
	if options.EnablePruning && options.PruningOptions != nil {
		prunedSubgraphs, err := o.PruneGraph(ctx, organized.Subgraphs, options.PruningOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to prune graph: %w", err)
		}
		organized.Subgraphs = prunedSubgraphs
	}

	// 重排序
	if options.EnableReranking && options.RerankingOptions != nil {
		reranked, err := o.Rerank(ctx, result, options.RerankingOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to rerank: %w", err)
		}
		organized.Entities = reranked.Entities
		organized.Triples = reranked.Triples
		organized.Subgraphs = reranked.Subgraphs
	}

	// 图增强
	if options.EnableAugmentation && options.AugmentationOptions != nil {
		augmented, err := o.AugmentGraph(ctx, organized.Subgraphs, options.AugmentationOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to augment graph: %w", err)
		}
		organized.Subgraphs = augmented
	}

	// 文本化
	if options.EnableVerbalization && options.VerbalizationOptions != nil {
		texts, err := o.Verbalize(ctx, organized.Subgraphs, options.VerbalizationOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to verbalize: %w", err)
		}
		organized.VerbalizedTexts = texts
	}

	return organized, nil
}

// PruneGraph 图剪枝
func (o *defaultOrganizer) PruneGraph(ctx context.Context, subgraphs []Subgraph, options *PruningOptions) ([]Subgraph, error) {
	if options == nil {
		return subgraphs, nil
	}

	pruned := make([]Subgraph, 0, len(subgraphs))

	for _, subgraph := range subgraphs {
		// 应用最小分数阈值
		if options.MinScore > 0 && subgraph.Score < options.MinScore {
			continue
		}

		// 限制节点数
		if options.MaxNodes > 0 && len(subgraph.Entities) > options.MaxNodes {
			// 按重要性排序并保留TopK
			subgraph = o.limitNodes(subgraph, options.MaxNodes, options.KeepCoreEntities)
		}

		// 限制边数
		if options.MaxEdges > 0 && len(subgraph.Triples) > options.MaxEdges {
			// 保留最重要的边
			subgraph.Triples = o.limitEdges(subgraph.Triples, options.MaxEdges)
		}

		pruned = append(pruned, subgraph)
	}

	return pruned, nil
}

// limitNodes 限制节点数量
func (o *defaultOrganizer) limitNodes(subgraph Subgraph, maxNodes int, keepCore bool) Subgraph {
	if len(subgraph.Entities) <= maxNodes {
		return subgraph
	}

	// 计算每个节点的重要性（基于度中心性）
	nodeScores := make(map[string]float64)
	for _, triple := range subgraph.Triples {
		nodeScores[triple.Subject]++
		nodeScores[triple.Object]++
	}

	// 确保根实体被保留
	if keepCore {
		nodeScores[subgraph.RootEntity] = math.MaxFloat64
	}

	// 按分数排序
	type nodeScore struct {
		id    string
		score float64
	}
	nodes := make([]nodeScore, 0, len(subgraph.Entities))
	for _, id := range subgraph.Entities {
		nodes = append(nodes, nodeScore{
			id:    id,
			score: nodeScores[id],
		})
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].score > nodes[j].score
	})

	// 保留TopK节点
	keptNodes := make(map[string]bool)
	keptEntities := make([]string, 0, maxNodes)
	for i := 0; i < maxNodes && i < len(nodes); i++ {
		keptNodes[nodes[i].id] = true
		keptEntities = append(keptEntities, nodes[i].id)
	}

	// 过滤三元组，只保留涉及保留节点的三元组
	filteredTriples := make([]Triple, 0)
	for _, triple := range subgraph.Triples {
		if keptNodes[triple.Subject] && keptNodes[triple.Object] {
			filteredTriples = append(filteredTriples, triple)
		}
	}

	return Subgraph{
		RootEntity: subgraph.RootEntity,
		Triples:    filteredTriples,
		Entities:   keptEntities,
		Score:      subgraph.Score,
		Metadata:   subgraph.Metadata,
	}
}

// limitEdges 限制边数量
func (o *defaultOrganizer) limitEdges(triples []Triple, maxEdges int) []Triple {
	if len(triples) <= maxEdges {
		return triples
	}

	// 计算每条边的重要性（基于节点度）
	edgeScores := make([]float64, len(triples))
	nodeDegrees := make(map[string]int)

	for _, triple := range triples {
		nodeDegrees[triple.Subject]++
		nodeDegrees[triple.Object]++
	}

	for i, triple := range triples {
		edgeScores[i] = float64(nodeDegrees[triple.Subject] + nodeDegrees[triple.Object])
	}

	// 创建索引并排序
	indices := make([]int, len(triples))
	for i := range indices {
		indices[i] = i
	}

	sort.Slice(indices, func(i, j int) bool {
		return edgeScores[indices[i]] > edgeScores[indices[j]]
	})

	// 保留TopK边
	result := make([]Triple, 0, maxEdges)
	for i := 0; i < maxEdges && i < len(indices); i++ {
		result = append(result, triples[indices[i]])
	}

	return result
}

// Rerank 重排序
func (o *defaultOrganizer) Rerank(ctx context.Context, result *RetrievalResult, options *RerankingOptions) (*RetrievalResult, error) {
	if options == nil {
		return result, nil
	}

	reranked := &RetrievalResult{
		Entities:   make([]RetrievedEntity, len(result.Entities)),
		Triples:    result.Triples,
		Subgraphs:  result.Subgraphs,
		Metadata:   result.Metadata,
		TotalScore: result.TotalScore,
	}

	copy(reranked.Entities, result.Entities)

	switch options.Method {
	case RerankByScore:
		// 按分数排序
		sort.Slice(reranked.Entities, func(i, j int) bool {
			return reranked.Entities[i].Score > reranked.Entities[j].Score
		})
	case RerankByCentrality:
		// 按中心性排序
		o.rerankByCentrality(reranked)
	case RerankByDiversity:
		// 按多样性排序
		o.rerankByDiversity(reranked)
	case RerankByHybrid:
		// 混合排序
		o.rerankByHybrid(reranked, options)
	default:
		// 默认按分数排序
		sort.Slice(reranked.Entities, func(i, j int) bool {
			return reranked.Entities[i].Score > reranked.Entities[j].Score
		})
	}

	// 应用TopK限制
	if options.TopK > 0 && len(reranked.Entities) > options.TopK {
		reranked.Entities = reranked.Entities[:options.TopK]
	}

	// 更新子图顺序
	entityMap := make(map[string]int)
	for i, e := range reranked.Entities {
		entityMap[e.EntityID] = i
	}

	sort.Slice(reranked.Subgraphs, func(i, j int) bool {
		scoreI := entityMap[reranked.Subgraphs[i].RootEntity]
		scoreJ := entityMap[reranked.Subgraphs[j].RootEntity]
		return scoreI < scoreJ
	})

	return reranked, nil
}

// rerankByCentrality 按中心性重排序
func (o *defaultOrganizer) rerankByCentrality(result *RetrievalResult) {
	// 计算每个实体的度中心性
	centrality := make(map[string]float64)
	for _, triple := range result.Triples {
		centrality[triple.Subject]++
		centrality[triple.Object]++
	}

	// 更新分数并排序
	for i := range result.Entities {
		if cent, ok := centrality[result.Entities[i].EntityID]; ok {
			result.Entities[i].Score = cent
		}
	}

	sort.Slice(result.Entities, func(i, j int) bool {
		return result.Entities[i].Score > result.Entities[j].Score
	})
}

// rerankByDiversity 按多样性重排序
func (o *defaultOrganizer) rerankByDiversity(result *RetrievalResult) {
	// 简单的多样性排序：确保结果覆盖不同的实体类型
	// 这里使用简单的启发式：优先选择与已选实体不同的实体
	selected := make(map[string]bool)
	diverseEntities := make([]RetrievedEntity, 0, len(result.Entities))

	// 按分数排序
	sorted := make([]RetrievedEntity, len(result.Entities))
	copy(sorted, result.Entities)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})

	for _, entity := range sorted {
		// 检查是否与已选实体有重叠
		hasOverlap := false
		for _, triple := range entity.Triples {
			if selected[triple.Subject] || selected[triple.Object] {
				hasOverlap = true
				break
			}
		}

		// 如果没有重叠或多样性要求不高，添加
		if !hasOverlap || len(diverseEntities) < 3 {
			diverseEntities = append(diverseEntities, entity)
			selected[entity.EntityID] = true
		}
	}

	result.Entities = diverseEntities
}

// rerankByHybrid 混合重排序
func (o *defaultOrganizer) rerankByHybrid(result *RetrievalResult, options *RerankingOptions) {
	// 结合分数和中心性
	centrality := make(map[string]float64)
	for _, triple := range result.Triples {
		centrality[triple.Subject]++
		centrality[triple.Object]++
	}

	// 归一化中心性
	maxCent := 0.0
	for _, cent := range centrality {
		if cent > maxCent {
			maxCent = cent
		}
	}

	// 混合分数
	for i := range result.Entities {
		entityID := result.Entities[i].EntityID
		cent := centrality[entityID]
		if maxCent > 0 {
			cent = cent / maxCent
		}
		// 加权组合：70% 原始分数，30% 中心性
		result.Entities[i].Score = 0.7*result.Entities[i].Score + 0.3*cent
	}

	sort.Slice(result.Entities, func(i, j int) bool {
		return result.Entities[i].Score > result.Entities[j].Score
	})
}

// AugmentGraph 图增强
func (o *defaultOrganizer) AugmentGraph(ctx context.Context, subgraphs []Subgraph, options *AugmentationOptions) ([]Subgraph, error) {
	if options == nil {
		return subgraphs, nil
	}

	augmented := make([]Subgraph, 0, len(subgraphs))

	for _, subgraph := range subgraphs {
		augmentedSubgraph := subgraph

		// 添加推理关系
		if options.AddInferredRelations {
			inferred := o.inferRelations(ctx, subgraph)
			augmentedSubgraph.Triples = append(augmentedSubgraph.Triples, inferred...)
		}

		// 添加相似实体
		if options.AddSimilarEntities {
			similar := o.findSimilarEntities(ctx, subgraph, options.MaxAugmentations)
			augmentedSubgraph.Entities = append(augmentedSubgraph.Entities, similar...)
		}

		augmented = append(augmented, augmentedSubgraph)
	}

	return augmented, nil
}

// inferRelations 推理关系
func (o *defaultOrganizer) inferRelations(ctx context.Context, subgraph Subgraph) []Triple {
	// 简单的推理：如果 A -> B 且 B -> C，则可能 A -> C（传递性）
	inferred := make([]Triple, 0)
	tripleMap := make(map[string]bool)

	// 建立索引
	for _, t := range subgraph.Triples {
		key := fmt.Sprintf("%s|%s|%s", t.Subject, t.Predicate, t.Object)
		tripleMap[key] = true
	}

	// 查找传递关系
	for _, t1 := range subgraph.Triples {
		for _, t2 := range subgraph.Triples {
			if t1.Object == t2.Subject && t1.Predicate == t2.Predicate {
				// 可能的传递关系
				inferredTriple := Triple{
					Subject:   t1.Subject,
					Predicate: t1.Predicate,
					Object:    t2.Object,
				}
				key := fmt.Sprintf("%s|%s|%s", inferredTriple.Subject, inferredTriple.Predicate, inferredTriple.Object)
				if !tripleMap[key] {
					inferred = append(inferred, inferredTriple)
					tripleMap[key] = true
				}
			}
		}
	}

	return inferred
}

// findSimilarEntities 查找相似实体
func (o *defaultOrganizer) findSimilarEntities(ctx context.Context, subgraph Subgraph, maxCount int) []string {
	// 这里简化实现，实际应该使用向量相似度
	// 返回空列表，实际实现需要访问embedding数据库
	return []string{}
}

// Verbalize 将图结构转换为文本
func (o *defaultOrganizer) Verbalize(ctx context.Context, subgraphs []Subgraph, options *VerbalizationOptions) ([]string, error) {
	if options == nil {
		options = &VerbalizationOptions{
			Format:          FormatNaturalLanguage,
			IncludeMetadata: false,
			MaxLength:       500,
		}
	}

	texts := make([]string, 0, len(subgraphs))

	for _, subgraph := range subgraphs {
		var text string

		switch options.Format {
		case FormatNaturalLanguage:
			text = o.verbalizeNaturalLanguage(subgraph, options)
		case FormatStructured:
			text = o.verbalizeStructured(subgraph, options)
		case FormatSummary:
			text = o.verbalizeSummary(subgraph, options)
		default:
			text = o.verbalizeNaturalLanguage(subgraph, options)
		}

		// 限制长度
		if options.MaxLength > 0 && len(text) > options.MaxLength {
			text = text[:options.MaxLength] + "..."
		}

		texts = append(texts, text)
	}

	return texts, nil
}

// verbalizeNaturalLanguage 自然语言文本化
func (o *defaultOrganizer) verbalizeNaturalLanguage(subgraph Subgraph, options *VerbalizationOptions) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("实体 %s 的相关信息：", subgraph.RootEntity))

	for _, triple := range subgraph.Triples {
		parts = append(parts, fmt.Sprintf("%s %s %s", triple.Subject, triple.Predicate, triple.Object))
	}

	return strings.Join(parts, "\n")
}

// verbalizeStructured 结构化文本化
func (o *defaultOrganizer) verbalizeStructured(subgraph Subgraph, options *VerbalizationOptions) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("Root: %s", subgraph.RootEntity))
	parts = append(parts, "Relations:")

	for _, triple := range subgraph.Triples {
		parts = append(parts, fmt.Sprintf("  - %s ->[%s]-> %s", triple.Subject, triple.Predicate, triple.Object))
	}

	return strings.Join(parts, "\n")
}

// verbalizeSummary 摘要格式文本化
func (o *defaultOrganizer) verbalizeSummary(subgraph Subgraph, options *VerbalizationOptions) string {
	return fmt.Sprintf("实体 %s 有 %d 个相关关系和 %d 个相关实体",
		subgraph.RootEntity, len(subgraph.Triples), len(subgraph.Entities))
}
