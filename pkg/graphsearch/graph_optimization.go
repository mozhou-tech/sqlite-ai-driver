package graphsearch

import (
	"context"
	"fmt"
)

// OptimizedSubgraphExtractor 优化的子图提取器
type OptimizedSubgraphExtractor struct {
	graphsearch *graphsearch
}

// NewOptimizedSubgraphExtractor 创建优化的子图提取器
func NewOptimizedSubgraphExtractor(gs *graphsearch) *OptimizedSubgraphExtractor {
	return &OptimizedSubgraphExtractor{
		graphsearch: gs,
	}
}

// GetOptimizedSubgraph 获取优化的子图（使用更高效的算法）
func (e *OptimizedSubgraphExtractor) GetOptimizedSubgraph(ctx context.Context, entityID string, maxDepth int) ([]Triple, error) {
	if maxDepth <= 0 {
		maxDepth = 2
	}

	// 使用优化的 BFS 算法
	return e.optimizedBFSSubgraph(ctx, entityID, maxDepth), nil
}

// optimizedBFSSubgraph 优化的 BFS 子图提取
func (e *OptimizedSubgraphExtractor) optimizedBFSSubgraph(ctx context.Context, entityID string, maxDepth int) []Triple {
	var triples []Triple
	visited := make(map[string]bool)
	tripleSet := make(map[string]bool)

	// 使用队列进行 BFS
	type nodeInfo struct {
		node  string
		depth int
	}
	queue := []nodeInfo{{entityID, 0}}
	visited[entityID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth {
			continue
		}

		// 获取当前节点的所有边
		query := e.graphsearch.graph.Query()
		allTriples, err := query.V(current.node).Both().All(ctx)
		if err != nil {
			continue
		}

		// 处理三元组
		for _, t := range allTriples {
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
					visited[nextNode] = true
					queue = append(queue, nodeInfo{nextNode, current.depth + 1})
				}
			}
		}
	}

	return triples
}

// FindShortestPath 查找最短路径（使用 Dijkstra 算法）
func (g *graphsearch) FindShortestPath(ctx context.Context, from, to string, maxDepth int) ([]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if maxDepth <= 0 {
		maxDepth = 10
	}

	// 使用 BFS 查找最短路径
	visited := make(map[string]bool)
	parent := make(map[string]string)
	queue := []string{from}
	visited[from] = true

	found := false
	depth := 0

	for len(queue) > 0 && depth < maxDepth {
		levelSize := len(queue)
		depth++

		for i := 0; i < levelSize; i++ {
			current := queue[0]
			queue = queue[1:]

			if current == to {
				found = true
				break
			}

			// 获取邻居
			query := g.graph.Query()
			allTriples, err := query.V(current).Both().All(ctx)
			if err != nil {
				continue
			}

			neighbors := make(map[string]bool)
			for _, t := range allTriples {
				var neighbor string
				if t.Subject == current {
					neighbor = t.Object
				} else {
					neighbor = t.Subject
				}
				neighbors[neighbor] = true
			}

			for neighbor := range neighbors {
				if !visited[neighbor] {
					visited[neighbor] = true
					parent[neighbor] = current
					queue = append(queue, neighbor)
				}
			}
		}

		if found {
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("path not found")
	}

	// 重构路径
	path := []string{to}
	current := to
	for current != from {
		if p, ok := parent[current]; ok {
			current = p
			path = append([]string{current}, path...)
		} else {
			return nil, fmt.Errorf("failed to reconstruct path")
		}
	}

	return path, nil
}

// FindAllPaths 查找所有路径（使用 DFS）
func (g *graphsearch) FindAllPaths(ctx context.Context, from, to string, maxDepth int, maxPaths int) ([][]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if maxDepth <= 0 {
		maxDepth = 5
	}
	if maxPaths <= 0 {
		maxPaths = 10
	}

	var paths [][]string
	visited := make(map[string]bool)

	var dfs func(current string, path []string, depth int)
	dfs = func(current string, path []string, depth int) {
		if len(paths) >= maxPaths {
			return
		}

		if current == to {
			// 找到一条路径
			newPath := make([]string, len(path))
			copy(newPath, path)
			paths = append(paths, newPath)
			return
		}

		if depth >= maxDepth {
			return
		}

		visited[current] = true
		path = append(path, current)

		// 获取邻居
		query := g.graph.Query()
		allTriples, err := query.V(current).Both().All(ctx)
		if err == nil {
			neighbors := make(map[string]bool)
			for _, t := range allTriples {
				var neighbor string
				if t.Subject == current {
					neighbor = t.Object
				} else {
					neighbor = t.Subject
				}
				neighbors[neighbor] = true
			}

			for neighbor := range neighbors {
				if !visited[neighbor] {
					dfs(neighbor, path, depth+1)
				}
			}
		}

		visited[current] = false
	}

	dfs(from, []string{}, 0)

	return paths, nil
}

// GetCentralEntities 获取中心实体（基于度中心性）
func (g *graphsearch) GetCentralEntities(ctx context.Context, limit int) ([]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if limit <= 0 {
		limit = 10
	}

	// 计算每个实体的度
	degree := make(map[string]int)

	// 获取所有三元组
	triples, err := g.AllTriples(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all triples: %w", err)
	}

	// 计算度
	for _, triple := range triples {
		degree[triple.Subject]++
		degree[triple.Object]++
	}

	// 按度排序
	type entityDegree struct {
		entity string
		degree int
	}
	entities := make([]entityDegree, 0, len(degree))
	for entity, deg := range degree {
		entities = append(entities, entityDegree{entity: entity, degree: deg})
	}

	// 简单排序
	for i := 0; i < len(entities)-1; i++ {
		for j := i + 1; j < len(entities); j++ {
			if entities[i].degree < entities[j].degree {
				entities[i], entities[j] = entities[j], entities[i]
			}
		}
	}

	// 返回 TopK
	result := make([]string, 0, limit)
	for i := 0; i < limit && i < len(entities); i++ {
		result = append(result, entities[i].entity)
	}

	return result, nil
}

// GetEntityNeighbors 获取实体的邻居（优化版）
func (g *graphsearch) GetEntityNeighbors(ctx context.Context, entityID string, maxNeighbors int) ([]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	neighborSet := make(map[string]bool)

	// 获取所有相关的三元组
	query := g.graph.Query()
	allTriples, err := query.V(entityID).Both().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get neighbors: %w", err)
	}

	for _, t := range allTriples {
		var neighbor string
		if t.Subject == entityID {
			neighbor = t.Object
		} else {
			neighbor = t.Subject
		}
		neighborSet[neighbor] = true
	}

	neighbors := make([]string, 0, len(neighborSet))
	for neighbor := range neighborSet {
		neighbors = append(neighbors, neighbor)
	}

	// 限制数量
	if maxNeighbors > 0 && len(neighbors) > maxNeighbors {
		neighbors = neighbors[:maxNeighbors]
	}

	return neighbors, nil
}
