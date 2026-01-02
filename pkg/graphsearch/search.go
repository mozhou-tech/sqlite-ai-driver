package graphsearch

import (
	"context"
	"encoding/json"
	"fmt"
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

	// 转换向量为字符串格式
	vectorStr := "["
	for i, val := range queryEmbedding {
		if i > 0 {
			vectorStr += ", "
		}
		vectorStr += fmt.Sprintf("%g", val)
	}
	vectorStr += "]"

	// 使用SQLite的向量相似度搜索
	// 向量检索使用 sqlite-driver 的 index.db 共享数据库
	sqlQuery := fmt.Sprintf(`
		SELECT 
			entity_id,
			entity_name,
			metadata,
			list_cosine_similarity(embedding, ?::FLOAT[]) as similarity
		FROM %s
		WHERE embedding IS NOT NULL AND embedding_status = 'completed'
		ORDER BY list_cosine_similarity(embedding, ?::FLOAT[]) DESC
		LIMIT ?
	`, g.tableName)

	rows, err := g.db.QueryContext(ctx, sqlQuery, vectorStr, vectorStr, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search entities: %w", err)
	}
	defer rows.Close()

	var results []SemanticSearchResult
	entityIDs := make(map[string]bool)

	for rows.Next() {
		var entityID, entityName string
		var metadataVal any
		var similarity float64

		err := rows.Scan(&entityID, &entityName, &metadataVal, &similarity)
		if err != nil {
			continue
		}

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

		// 获取该实体在图谱中的关系（子图）
		triples := g.getSubgraphTriples(ctx, entityID, maxDepth)

		results = append(results, SemanticSearchResult{
			EntityID:   entityID,
			EntityName: entityName,
			Score:      similarity,
			Metadata:   metadata,
			Triples:    triples,
		})

		entityIDs[entityID] = true
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
