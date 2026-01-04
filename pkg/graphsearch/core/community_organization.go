package graphsearch

import (
	"context"
	"fmt"
	"math"
)

// CommunityOrganizer 基于社区的组织器
type CommunityOrganizer struct {
	*defaultOrganizer
}

// NewCommunityOrganizer 创建基于社区的组织器
func NewCommunityOrganizer(gs *graphsearch) *CommunityOrganizer {
	return &CommunityOrganizer{
		defaultOrganizer: NewDefaultOrganizer(gs).(*defaultOrganizer),
	}
}

// OrganizeByCommunity 基于社区组织检索结果
func (o *CommunityOrganizer) OrganizeByCommunity(ctx context.Context, result *RetrievalResult, options *CommunityOrganizationOptions) (*OrganizedResult, error) {
	if result == nil {
		return nil, fmt.Errorf("retrieval result is nil")
	}

	if options == nil {
		options = DefaultCommunityOrganizationOptions()
	}

	// 检测社区
	communities, err := o.detectCommunitiesFromResult(ctx, result, options)
	if err != nil {
		return nil, fmt.Errorf("failed to detect communities: %w", err)
	}

	// 按社区组织结果
	organized := &OrganizedResult{
		Entities:  make([]RetrievedEntity, 0),
		Triples:   result.Triples,
		Subgraphs: make([]Subgraph, 0),
		Metadata:  result.Metadata,
	}

	// 为每个社区创建子图
	for _, community := range communities {
		// 收集社区中的实体
		communityEntities := make([]RetrievedEntity, 0)
		entitySet := make(map[string]bool)

		for _, entityID := range community.Entities {
			// 查找对应的检索实体
			for _, entity := range result.Entities {
				if entity.EntityID == entityID {
					communityEntities = append(communityEntities, entity)
					entitySet[entityID] = true
					break
				}
			}
		}

		// 收集社区中的三元组
		communityTriples := make([]Triple, 0)
		for _, triple := range result.Triples {
			if entitySet[triple.Subject] || entitySet[triple.Object] {
				communityTriples = append(communityTriples, triple)
			}
		}

		// 计算社区分数
		communityScore := o.calculateCommunityScore(communityEntities, communityTriples, options)

		// 创建社区子图
		rootEntity := community.Entities[0]
		if len(community.Entities) > 0 {
			// 选择分数最高的实体作为根实体
			maxScore := 0.0
			for _, entity := range communityEntities {
				if entity.Score > maxScore {
					maxScore = entity.Score
					rootEntity = entity.EntityID
				}
			}
		}

		subgraph := Subgraph{
			RootEntity: rootEntity,
			Triples:    communityTriples,
			Entities:   community.Entities,
			Score:      communityScore,
			Metadata: map[string]any{
				"community_id":   community.ID,
				"entity_count":   len(community.Entities),
				"relation_count": len(communityTriples),
			},
		}

		organized.Subgraphs = append(organized.Subgraphs, subgraph)
		organized.Entities = append(organized.Entities, communityEntities...)
	}

	// 去重实体
	organized.Entities = o.deduplicateEntities(organized.Entities)

	// 去重三元组
	organized.Triples = o.deduplicateTriples(organized.Triples)

	// 按社区分数排序子图
	organized.Subgraphs = o.sortSubgraphsByScore(organized.Subgraphs)

	return organized, nil
}

// CommunityOrganizationOptions 社区组织选项
type CommunityOrganizationOptions struct {
	// MinCommunitySize 最小社区大小
	MinCommunitySize int
	// MaxCommunitySize 最大社区大小
	MaxCommunitySize int
	// CommunityDetectionMethod 社区检测方法
	CommunityDetectionMethod string
	// UseEntitySimilarity 是否使用实体相似度
	UseEntitySimilarity bool
	// SimilarityThreshold 相似度阈值
	SimilarityThreshold float64
}

// DefaultCommunityOrganizationOptions 返回默认的社区组织选项
func DefaultCommunityOrganizationOptions() *CommunityOrganizationOptions {
	return &CommunityOrganizationOptions{
		MinCommunitySize:         2,
		MaxCommunitySize:         50,
		CommunityDetectionMethod: "connected_components",
		UseEntitySimilarity:      false,
		SimilarityThreshold:      0.5,
	}
}

// detectCommunitiesFromResult 从检索结果中检测社区
func (o *CommunityOrganizer) detectCommunitiesFromResult(ctx context.Context, result *RetrievalResult, options *CommunityOrganizationOptions) ([]Community, error) {
	// 构建图
	graph := make(map[string]map[string]bool)
	visited := make(map[string]bool)

	// 初始化图
	for _, entity := range result.Entities {
		entityID := entity.EntityID
		graph[entityID] = make(map[string]bool)
		visited[entityID] = false
	}

	// 添加边
	for _, triple := range result.Triples {
		if graph[triple.Subject] == nil {
			graph[triple.Subject] = make(map[string]bool)
			visited[triple.Subject] = false
		}
		if graph[triple.Object] == nil {
			graph[triple.Object] = make(map[string]bool)
			visited[triple.Object] = false
		}
		graph[triple.Subject][triple.Object] = true
		graph[triple.Object][triple.Subject] = true
	}

	// 使用连通分量算法检测社区
	communities := make([]Community, 0)
	communityID := 0

	for entityID := range graph {
		if !visited[entityID] {
			communityID++
			community := Community{
				ID:       fmt.Sprintf("community_%d", communityID),
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

			// 过滤社区大小
			if len(community.Entities) >= options.MinCommunitySize {
				if options.MaxCommunitySize > 0 && len(community.Entities) > options.MaxCommunitySize {
					// 截断社区（保留分数最高的实体）
					community.Entities = o.selectTopEntities(community.Entities, result.Entities, options.MaxCommunitySize)
				}
				communities = append(communities, community)
			}
		}
	}

	return communities, nil
}

// calculateCommunityScore 计算社区分数
func (o *CommunityOrganizer) calculateCommunityScore(entities []RetrievedEntity, triples []Triple, options *CommunityOrganizationOptions) float64 {
	if len(entities) == 0 {
		return 0.0
	}

	// 计算平均实体分数
	var totalScore float64
	for _, entity := range entities {
		totalScore += entity.Score
	}
	avgEntityScore := totalScore / float64(len(entities))

	// 计算关系密度
	relationDensity := float64(len(triples)) / float64(len(entities))
	if len(entities) > 1 {
		relationDensity = relationDensity / float64(len(entities)-1)
	}

	// 综合分数
	communityScore := 0.7*avgEntityScore + 0.3*math.Min(relationDensity, 1.0)

	return communityScore
}

// selectTopEntities 选择 TopK 实体
func (o *CommunityOrganizer) selectTopEntities(entityIDs []string, allEntities []RetrievedEntity, topK int) []string {
	// 创建实体分数映射
	scoreMap := make(map[string]float64)
	for _, entity := range allEntities {
		scoreMap[entity.EntityID] = entity.Score
	}

	// 按分数排序
	type entityScore struct {
		id    string
		score float64
	}
	scores := make([]entityScore, 0, len(entityIDs))
	for _, id := range entityIDs {
		score := scoreMap[id]
		if score == 0 {
			score = 0.5 // 默认分数
		}
		scores = append(scores, entityScore{id: id, score: score})
	}

	// 简单排序（冒泡排序）
	for i := 0; i < len(scores)-1; i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].score < scores[j].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	// 选择 TopK
	if len(scores) > topK {
		scores = scores[:topK]
	}

	result := make([]string, 0, len(scores))
	for _, s := range scores {
		result = append(result, s.id)
	}

	return result
}

// deduplicateEntities 去重实体
func (o *CommunityOrganizer) deduplicateEntities(entities []RetrievedEntity) []RetrievedEntity {
	entityMap := make(map[string]*RetrievedEntity)

	for _, entity := range entities {
		if existing, ok := entityMap[entity.EntityID]; ok {
			// 合并分数（取最大值）
			if entity.Score > existing.Score {
				existing.Score = entity.Score
			}
			// 合并三元组
			existing.Triples = append(existing.Triples, entity.Triples...)
		} else {
			entityMap[entity.EntityID] = &RetrievedEntity{
				EntityID:   entity.EntityID,
				EntityName: entity.EntityName,
				Score:      entity.Score,
				Metadata:   entity.Metadata,
				Triples:    entity.Triples,
			}
		}
	}

	result := make([]RetrievedEntity, 0, len(entityMap))
	for _, entity := range entityMap {
		// 去重三元组
		entity.Triples = o.deduplicateTriples(entity.Triples)
		result = append(result, *entity)
	}

	return result
}

// deduplicateTriples 去重三元组（从 defaultOrganizer 复制）
func (o *CommunityOrganizer) deduplicateTriples(triples []Triple) []Triple {
	tripleMap := make(map[string]bool)
	var uniqueTriples []Triple

	for _, t := range triples {
		key := fmt.Sprintf("%s|%s|%s", t.Subject, t.Predicate, t.Object)
		if !tripleMap[key] {
			tripleMap[key] = true
			uniqueTriples = append(uniqueTriples, t)
		}
	}

	return uniqueTriples
}

// sortSubgraphsByScore 按分数排序子图
func (o *CommunityOrganizer) sortSubgraphsByScore(subgraphs []Subgraph) []Subgraph {
	// 简单排序（冒泡排序）
	sorted := make([]Subgraph, len(subgraphs))
	copy(sorted, subgraphs)

	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].Score < sorted[j].Score {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}
