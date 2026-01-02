package graphsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// SemanticSearch 语义检索图谱
// 通过查询文本找到相似的实体，然后返回这些实体在图谱中的关系
// query: 查询文本
// limit: 返回的实体数量限制
// maxDepth: 图谱遍历的最大深度（用于获取子图）
func (g *graphsearch) SemanticSearch(ctx context.Context, query string, limit int, maxDepth int) ([]SemanticSearchResult, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if g.embedder == nil {
		return nil, fmt.Errorf("embedder not provided")
	}

	if limit <= 0 {
		limit = 10
	}

	if maxDepth <= 0 {
		maxDepth = 2
	}

	// 生成查询向量
	queryEmbedding, err := g.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	}

	// 查询所有候选实体（在内存中计算相似度）
	// SQLite 不支持 list_cosine_similarity 函数，需要在内存中计算
	sqlQuery := fmt.Sprintf(`
		SELECT 
			entity_id,
			entity_name,
			metadata,
			embedding
		FROM %s
		WHERE embedding IS NOT NULL AND embedding_status = 'completed'
	`, g.tableName)

	rows, err := g.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to search entities: %w", err)
	}
	defer rows.Close()

	// 在内存中计算余弦相似度并排序
	type candidateResult struct {
		entityID   string
		entityName string
		metadata   map[string]any
		similarity float64
	}

	var candidates []candidateResult

	for rows.Next() {
		var entityID, entityName string
		var metadataVal any
		var embeddingVal any

		err := rows.Scan(&entityID, &entityName, &metadataVal, &embeddingVal)
		if err != nil {
			continue
		}

		// 解析 embedding
		var storedEmbedding []float64
		if embeddingVal != nil {
			// 尝试不同的格式：字符串、JSON、字节数组
			switch v := embeddingVal.(type) {
			case string:
				// 尝试解析 JSON 格式的向量（格式：[1.0, 2.0, 3.0]）
				if err := json.Unmarshal([]byte(v), &storedEmbedding); err != nil {
					continue
				}
			case []byte:
				if err := json.Unmarshal(v, &storedEmbedding); err != nil {
					continue
				}
			default:
				// 尝试转换为字符串再解析
				if str, ok := v.(string); ok {
					if err := json.Unmarshal([]byte(str), &storedEmbedding); err != nil {
						continue
					}
				} else {
					continue
				}
			}
		}

		if len(storedEmbedding) == 0 || len(storedEmbedding) != len(queryEmbedding) {
			continue
		}

		// 计算余弦相似度
		similarity := cosineSimilarity(queryEmbedding, storedEmbedding)

		// 解析metadata
		var metadata map[string]any
		if metadataVal != nil {
			if metadataStr, ok := metadataVal.(string); ok {
				_ = json.Unmarshal([]byte(metadataStr), &metadata)
			}
		}
		if metadata == nil {
			metadata = make(map[string]any)
		}

		candidates = append(candidates, candidateResult{
			entityID:   entityID,
			entityName: entityName,
			metadata:   metadata,
			similarity: similarity,
		})
	}

	// 按相似度降序排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].similarity > candidates[j].similarity
	})

	// 取前 limit 个结果
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	// 转换为 SemanticSearchResult
	var results []SemanticSearchResult
	entityIDs := make(map[string]bool)

	for _, cand := range candidates {
		// 获取该实体在图谱中的关系（子图）
		triples := g.getSubgraphTriples(ctx, cand.entityID, maxDepth)

		results = append(results, SemanticSearchResult{
			EntityID:   cand.entityID,
			EntityName: cand.entityName,
			Score:      cand.similarity,
			Metadata:   cand.metadata,
			Triples:    triples,
		})

		entityIDs[cand.entityID] = true
	}

	return results, nil
}

// getSubgraphTriples 获取实体的子图三元组
func (g *graphsearch) getSubgraphTriples(ctx context.Context, entityID string, maxDepth int) []Triple {
	var triples []Triple
	visited := make(map[string]bool)
	tripleSet := make(map[string]bool) // 用于去重：key = "subject|predicate|object"
	queue := []struct {
		node  string
		depth int
	}{{entityID, 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth || visited[current.node] {
			continue
		}
		visited[current.node] = true

		// 使用 Query API 的 Both() 方法获取所有相关的三元组
		query := g.graph.Query()
		allTriples, err := query.V(current.node).Both().All(ctx)
		if err != nil {
			continue
		}

		// 处理获取到的三元组
		for _, t := range allTriples {
			// 创建唯一键用于去重
			key := fmt.Sprintf("%s|%s|%s", t.Subject, t.Predicate, t.Object)
			if !tripleSet[key] {
				tripleSet[key] = true
				triples = append(triples, Triple{
					Subject:   t.Subject,
					Predicate: t.Predicate,
					Object:    t.Object,
				})

				// 确定下一个要遍历的节点
				var nextNode string
				if t.Subject == current.node {
					nextNode = t.Object
				} else {
					nextNode = t.Subject
				}

				// 如果还没访问过且深度未超限，加入队列
				if !visited[nextNode] && current.depth < maxDepth-1 {
					queue = append(queue, struct {
						node  string
						depth int
					}{nextNode, current.depth + 1})
				}
			}
		}
	}

	return triples
}

// GetSubgraph 获取子图
// entityID: 实体ID
// maxDepth: 最大深度
func (g *graphsearch) GetSubgraph(ctx context.Context, entityID string, maxDepth int) ([]Triple, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.getSubgraphTriples(ctx, entityID, maxDepth), nil
}

// cosineSimilarity 计算两个向量的余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
