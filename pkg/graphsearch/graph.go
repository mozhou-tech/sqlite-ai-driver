package graphsearch

import (
	"context"
	"fmt"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
)

// Link 创建一条从 subject 到 object 的边，边的类型为 predicate
func (g *graphsearch) Link(ctx context.Context, subject, predicate, object string) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.Link(ctx, subject, predicate, object)
}

// Unlink 删除一条边
func (g *graphsearch) Unlink(ctx context.Context, subject, predicate, object string) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.Unlink(ctx, subject, predicate, object)
}

// GetNeighbors 获取指定节点的邻居节点
func (g *graphsearch) GetNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.GetNeighbors(ctx, node, predicate)
}

// GetInNeighbors 获取指向指定节点的邻居节点（入边）
func (g *graphsearch) GetInNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.GetInNeighbors(ctx, node, predicate)
}

// Query 返回查询构建器，支持类似 Gremlin 的查询语法
func (g *graphsearch) Query() cayley_driver.GraphQuery {
	if !g.initialized {
		return nil
	}
	return g.graph.Query()
}

// FindPath 查找从 from 到 to 的路径
func (g *graphsearch) FindPath(ctx context.Context, from, to string, maxDepth int, predicate string) ([][]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.FindPath(ctx, from, to, maxDepth, predicate)
}

// AllTriples 获取图中所有的三元组
func (g *graphsearch) AllTriples(ctx context.Context) ([]Triple, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	triples, err := g.graph.AllTriples(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]Triple, len(triples))
	for i, t := range triples {
		result[i] = Triple{
			Subject:   t.Subject,
			Predicate: t.Predicate,
			Object:    t.Object,
		}
	}

	return result, nil
}
