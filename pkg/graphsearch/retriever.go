package graphsearch

import (
	"context"
	"fmt"
	"math"
)

// defaultRetriever 默认检索器实现
type defaultRetriever struct {
	graphsearch *graphsearch
}

// NewDefaultRetriever 创建默认检索器
func NewDefaultRetriever(gs *graphsearch) Retriever {
	return &defaultRetriever{
		graphsearch: gs,
	}
}

// Retrieve 执行检索，返回相关实体和三元组
func (r *defaultRetriever) Retrieve(ctx context.Context, query *StructuredQuery, options *RetrievalOptions) (*RetrievalResult, error) {
	if options == nil {
		options = &RetrievalOptions{
			Limit:    10,
			MaxDepth: 2,
			Strategy: StrategyHybrid,
		}
	}

	var result *RetrievalResult
	var err error

	// 根据策略选择检索方法
	switch options.Strategy {
	case StrategyHeuristicOnly:
		result, err = r.HeuristicRetrieve(ctx, query, options)
	case StrategyLearningOnly:
		result, err = r.LearningRetrieve(ctx, query, options)
	case StrategyHybrid:
		// 混合策略：同时使用两种方法并合并结果
		heuristicResult, err1 := r.HeuristicRetrieve(ctx, query, options)
		learningResult, err2 := r.LearningRetrieve(ctx, query, options)

		if err1 != nil && err2 != nil {
			return nil, fmt.Errorf("both retrieval methods failed: %v, %v", err1, err2)
		}

		result = r.mergeResults(heuristicResult, learningResult, options)
	case StrategyAdaptive:
		// 自适应策略：根据查询特征选择最佳方法
		if len(query.Entities) > 0 {
			// 如果有明确的实体，优先使用启发式
			result, err = r.HeuristicRetrieve(ctx, query, options)
		} else {
			// 否则使用学习式
			result, err = r.LearningRetrieve(ctx, query, options)
		}
	default:
		result, err = r.LearningRetrieve(ctx, query, options)
	}

	if err != nil {
		return nil, err
	}

	return result, nil
}

// HeuristicRetrieve 基于启发式的检索
// 使用查询中的实体和关系直接在图谱中查找
func (r *defaultRetriever) HeuristicRetrieve(ctx context.Context, query *StructuredQuery, options *RetrievalOptions) (*RetrievalResult, error) {
	if !r.graphsearch.initialized {
		return nil, fmt.Errorf("store not initialized")
	}

	var entities []RetrievedEntity
	var triples []Triple
	entityMap := make(map[string]*RetrievedEntity)

	// 基于提取的实体进行检索
	for _, queryEntity := range query.Entities {
		// 获取实体的子图
		subgraphTriples := r.graphsearch.getSubgraphTriples(ctx, queryEntity.Name, options.MaxDepth)

		// 收集所有相关的三元组
		triples = append(triples, subgraphTriples...)

		// 创建检索到的实体
		if _, exists := entityMap[queryEntity.Name]; !exists {
			entityMap[queryEntity.Name] = &RetrievedEntity{
				EntityID:   queryEntity.Name,
				EntityName: queryEntity.Name,
				Score:      queryEntity.Confidence,
				Metadata:   queryEntity.Metadata,
				Triples:    subgraphTriples,
			}
		}
	}

	// 基于提取的关系进行检索
	for _, relation := range query.Relations {
		// 查找匹配的三元组
		query := r.graphsearch.graph.Query()
		results, err := query.V(relation.Subject).Out(relation.Predicate).All(ctx)
		if err == nil {
			for _, t := range results {
				if t.Object == relation.Object {
					triples = append(triples, Triple{
						Subject:   t.Subject,
						Predicate: t.Predicate,
						Object:    t.Object,
					})
				}
			}
		}
	}

	// 转换为切片
	for _, e := range entityMap {
		entities = append(entities, *e)
	}

	// 去重三元组
	triples = r.deduplicateTriples(triples)

	// 构建子图
	subgraphs := r.buildSubgraphs(entities, triples)

	return &RetrievalResult{
		Entities:   entities,
		Triples:    triples,
		Subgraphs:  subgraphs,
		Metadata:   make(map[string]any),
		TotalScore: r.calculateTotalScore(entities),
	}, nil
}

// LearningRetrieve 基于学习的检索（使用向量相似度）
// 这是对现有 SemanticSearch 的封装和扩展
func (r *defaultRetriever) LearningRetrieve(ctx context.Context, query *StructuredQuery, options *RetrievalOptions) (*RetrievalResult, error) {
	if !r.graphsearch.initialized {
		return nil, fmt.Errorf("store not initialized")
	}

	if r.graphsearch.embedder == nil {
		return nil, fmt.Errorf("embedder not provided")
	}

	limit := options.Limit
	if limit <= 0 {
		limit = 10
	}

	maxDepth := options.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 2
	}

	// 使用原始查询进行语义检索
	semanticResults, err := r.graphsearch.SemanticSearch(ctx, query.OriginalQuery, limit, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("semantic search failed: %w", err)
	}

	// 转换为检索结果格式
	var entities []RetrievedEntity
	var triples []Triple
	entityMap := make(map[string]bool)

	for _, result := range semanticResults {
		// 应用相似度阈值过滤
		if options.SimilarityThreshold > 0 && result.Score < options.SimilarityThreshold {
			continue
		}

		entities = append(entities, RetrievedEntity{
			EntityID:   result.EntityID,
			EntityName: result.EntityName,
			Score:      result.Score,
			Metadata:   result.Metadata,
			Triples:    result.Triples,
		})

		// 收集所有三元组
		triples = append(triples, result.Triples...)
		entityMap[result.EntityID] = true
	}

	// 去重三元组
	triples = r.deduplicateTriples(triples)

	// 构建子图
	subgraphs := r.buildSubgraphs(entities, triples)

	return &RetrievalResult{
		Entities:   entities,
		Triples:    triples,
		Subgraphs:  subgraphs,
		Metadata:   make(map[string]any),
		TotalScore: r.calculateTotalScore(entities),
	}, nil
}

// mergeResults 合并多个检索结果
func (r *defaultRetriever) mergeResults(result1, result2 *RetrievalResult, options *RetrievalOptions) *RetrievalResult {
	if result1 == nil {
		return result2
	}
	if result2 == nil {
		return result1
	}

	// 合并实体（去重并合并分数）
	entityMap := make(map[string]*RetrievedEntity)

	for _, e := range result1.Entities {
		entityMap[e.EntityID] = &RetrievedEntity{
			EntityID:   e.EntityID,
			EntityName: e.EntityName,
			Score:      e.Score,
			Metadata:   e.Metadata,
			Triples:    e.Triples,
		}
	}

	for _, e := range result2.Entities {
		if existing, ok := entityMap[e.EntityID]; ok {
			// 合并分数（取最大值或平均值）
			existing.Score = math.Max(existing.Score, e.Score)
			// 合并三元组
			existing.Triples = append(existing.Triples, e.Triples...)
		} else {
			entityMap[e.EntityID] = &RetrievedEntity{
				EntityID:   e.EntityID,
				EntityName: e.EntityName,
				Score:      e.Score,
				Metadata:   e.Metadata,
				Triples:    e.Triples,
			}
		}
	}

	// 转换为切片
	var entities []RetrievedEntity
	for _, e := range entityMap {
		entities = append(entities, *e)
	}

	// 合并三元组
	triples := append(result1.Triples, result2.Triples...)
	triples = r.deduplicateTriples(triples)

	// 构建子图
	subgraphs := r.buildSubgraphs(entities, triples)

	return &RetrievalResult{
		Entities:   entities,
		Triples:    triples,
		Subgraphs:  subgraphs,
		Metadata:   make(map[string]any),
		TotalScore: r.calculateTotalScore(entities),
	}
}

// buildSubgraphs 从实体和三元组构建子图
func (r *defaultRetriever) buildSubgraphs(entities []RetrievedEntity, triples []Triple) []Subgraph {
	subgraphs := make([]Subgraph, 0, len(entities))

	for _, entity := range entities {
		// 收集与该实体相关的所有三元组
		entityTriples := make([]Triple, 0)
		entitySet := make(map[string]bool)
		entitySet[entity.EntityID] = true

		for _, triple := range triples {
			if triple.Subject == entity.EntityID || triple.Object == entity.EntityID {
				entityTriples = append(entityTriples, triple)
				entitySet[triple.Subject] = true
				entitySet[triple.Object] = true
			}
		}

		// 转换为实体ID列表
		entityList := make([]string, 0, len(entitySet))
		for id := range entitySet {
			entityList = append(entityList, id)
		}

		subgraphs = append(subgraphs, Subgraph{
			RootEntity: entity.EntityID,
			Triples:    entityTriples,
			Entities:   entityList,
			Score:      entity.Score,
			Metadata:   entity.Metadata,
		})
	}

	return subgraphs
}

// deduplicateTriples 去重三元组
func (r *defaultRetriever) deduplicateTriples(triples []Triple) []Triple {
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

// calculateTotalScore 计算总体相关性分数
func (r *defaultRetriever) calculateTotalScore(entities []RetrievedEntity) float64 {
	if len(entities) == 0 {
		return 0.0
	}

	var totalScore float64
	for _, e := range entities {
		totalScore += e.Score
	}

	return totalScore / float64(len(entities))
}
