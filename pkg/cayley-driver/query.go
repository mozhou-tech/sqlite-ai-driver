package cayley_driver

import (
	"context"
	"database/sql"
	"fmt"
)

// graphQuery 实现 GraphQuery 接口
type graphQuery struct {
	graph     *cayleyGraph
	startNode string
	steps     []queryStep
}

type queryStep struct {
	direction string // "out" 或 "in"
	predicate string
}

// V 选择指定的节点
func (q *graphQuery) V(node string) GraphQuery {
	return &graphQuery{
		graph:     q.graph,
		startNode: node,
		steps:     q.steps,
	}
}

// Out 沿着指定的边类型向外遍历
func (q *graphQuery) Out(predicate string) GraphQuery {
	return &graphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "out",
			predicate: predicate,
		}),
	}
}

// In 沿着指定的边类型向内遍历
func (q *graphQuery) In(predicate string) GraphQuery {
	return &graphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "in",
			predicate: predicate,
		}),
	}
}

// Both 沿着所有边类型双向遍历（出边和入边）
func (q *graphQuery) Both() GraphQuery {
	return &graphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "both",
			predicate: "",
		}),
	}
}

// All 执行查询并返回所有结果
func (q *graphQuery) All(ctx context.Context) ([]Triple, error) {
	if q.startNode == "" {
		return nil, fmt.Errorf("query must start with V(node)")
	}

	// 如果没有步骤，返回空结果
	if len(q.steps) == 0 {
		return []Triple{}, nil
	}

	var results []Triple
	currentNodes := []string{q.startNode}

	// 遍历所有步骤
	for i, step := range q.steps {
		var nextNodes []string
		var triples []Triple

		for _, node := range currentNodes {
			if step.direction == "out" {
				neighbors, err := q.graph.GetNeighbors(ctx, node, step.predicate)
				if err != nil {
					return nil, err
				}
				for _, neighbor := range neighbors {
					triples = append(triples, Triple{
						Subject:   node,
						Predicate: step.predicate,
						Object:    neighbor,
					})
					nextNodes = append(nextNodes, neighbor)
				}
			} else if step.direction == "in" {
				neighbors, err := q.graph.GetInNeighbors(ctx, node, step.predicate)
				if err != nil {
					return nil, err
				}
				for _, neighbor := range neighbors {
					triples = append(triples, Triple{
						Subject:   neighbor,
						Predicate: step.predicate,
						Object:    node,
					})
					nextNodes = append(nextNodes, neighbor)
				}
			} else if step.direction == "both" {
				// 获取所有出边和入边（predicate为空时返回所有类型的边）
				// 查询所有以node为subject的三元组（出边）
				tableName := q.graph.tableName()
				outQuery := fmt.Sprintf(`SELECT predicate, object FROM %s WHERE subject = ?`, tableName)
				if step.predicate != "" {
					outQuery = fmt.Sprintf(`SELECT predicate, object FROM %s WHERE subject = ? AND predicate = ?`, tableName)
				}
				var outRows *sql.Rows
				var err error
				if step.predicate != "" {
					outRows, err = q.graph.getDB().QueryContext(ctx, outQuery, node, step.predicate)
				} else {
					outRows, err = q.graph.getDB().QueryContext(ctx, outQuery, node)
				}
				if err != nil {
					return nil, err
				}
				for outRows.Next() {
					var pred, obj string
					if err := outRows.Scan(&pred, &obj); err != nil {
						outRows.Close()
						return nil, err
					}
					triples = append(triples, Triple{
						Subject:   node,
						Predicate: pred,
						Object:    obj,
					})
					nextNodes = append(nextNodes, obj)
				}
				outRows.Close()

				// 查询所有以node为object的三元组（入边）
				inQuery := fmt.Sprintf(`SELECT subject, predicate FROM %s WHERE object = ?`, tableName)
				if step.predicate != "" {
					inQuery = fmt.Sprintf(`SELECT subject, predicate FROM %s WHERE object = ? AND predicate = ?`, tableName)
				}
				var inRows *sql.Rows
				if step.predicate != "" {
					inRows, err = q.graph.getDB().QueryContext(ctx, inQuery, node, step.predicate)
				} else {
					inRows, err = q.graph.getDB().QueryContext(ctx, inQuery, node)
				}
				if err != nil {
					return nil, err
				}
				for inRows.Next() {
					var subj, pred string
					if err := inRows.Scan(&subj, &pred); err != nil {
						inRows.Close()
						return nil, err
					}
					triples = append(triples, Triple{
						Subject:   subj,
						Predicate: pred,
						Object:    node,
					})
					nextNodes = append(nextNodes, subj)
				}
				inRows.Close()
			}
		}

		// 如果是最后一步，收集所有三元组
		if i == len(q.steps)-1 {
			results = triples
		}

		currentNodes = nextNodes
	}

	return results, nil
}

// Values 执行查询并返回所有节点值
func (q *graphQuery) Values(ctx context.Context) ([]string, error) {
	if q.startNode == "" {
		return nil, fmt.Errorf("query must start with V(node)")
	}

	currentNodes := []string{q.startNode}

	for _, step := range q.steps {
		var nextNodes []string

		for _, node := range currentNodes {
			if step.direction == "out" {
				neighbors, err := q.graph.GetNeighbors(ctx, node, step.predicate)
				if err != nil {
					return nil, err
				}
				nextNodes = append(nextNodes, neighbors...)
			} else if step.direction == "in" {
				neighbors, err := q.graph.GetInNeighbors(ctx, node, step.predicate)
				if err != nil {
					return nil, err
				}
				nextNodes = append(nextNodes, neighbors...)
			} else if step.direction == "both" {
				// 获取出边和入边的所有邻居
				outNeighbors, err := q.graph.GetNeighbors(ctx, node, step.predicate)
				if err != nil {
					return nil, err
				}
				nextNodes = append(nextNodes, outNeighbors...)
				inNeighbors, err := q.graph.GetInNeighbors(ctx, node, step.predicate)
				if err != nil {
					return nil, err
				}
				nextNodes = append(nextNodes, inNeighbors...)
			}
		}

		// 去重
		seen := make(map[string]bool)
		var uniqueNodes []string
		for _, node := range nextNodes {
			if !seen[node] {
				seen[node] = true
				uniqueNodes = append(uniqueNodes, node)
			}
		}
		currentNodes = uniqueNodes
	}

	return currentNodes, nil
}
