package cayley_driver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Triple 表示图数据库中的三元组（subject-predicate-object）
type Triple struct {
	Subject   string
	Predicate string
	Object    string
}

// Graph 定义图数据库的接口
type Graph interface {
	// Link 创建一条从 subject 到 object 的边，边的类型为 predicate
	Link(ctx context.Context, subject, predicate, object string) error

	// Unlink 删除一条边
	Unlink(ctx context.Context, subject, predicate, object string) error

	// GetNeighbors 获取指定节点的邻居节点
	// node: 节点ID
	// predicate: 边的类型，如果为空则返回所有类型的邻居
	GetNeighbors(ctx context.Context, node, predicate string) ([]string, error)

	// GetInNeighbors 获取指向指定节点的邻居节点（入边）
	GetInNeighbors(ctx context.Context, node, predicate string) ([]string, error)

	// Query 返回查询构建器，支持类似 Gremlin 的查询语法
	Query() GraphQuery

	// FindPath 查找从 from 到 to 的路径
	// maxDepth: 最大深度
	// predicate: 边的类型，如果为空则允许所有类型的边
	FindPath(ctx context.Context, from, to string, maxDepth int, predicate string) ([][]string, error)

	// AllTriples 获取图中所有的三元组
	AllTriples(ctx context.Context) ([]Triple, error)

	// Close 关闭图数据库连接
	Close() error
}

// GraphQuery 定义图查询构建器的接口
type GraphQuery interface {
	// V 选择指定的节点
	V(node string) GraphQuery

	// Out 沿着指定的边类型向外遍历
	Out(predicate string) GraphQuery

	// In 沿着指定的边类型向内遍历
	In(predicate string) GraphQuery

	// Both 沿着所有边类型双向遍历（出边和入边）
	Both() GraphQuery

	// All 执行查询并返回所有结果
	All(ctx context.Context) ([]Triple, error)

	// Values 执行查询并返回所有节点值
	Values(ctx context.Context) ([]string, error)
}

// cayleyGraph 是基于 SQLite3 的图数据库实现
type cayleyGraph struct {
	db *sql.DB
}

// getDataDir 获取基础数据目录
// 优先从环境变量 DATA_DIR 获取，如果未设置则使用默认值 ./data
func getDataDir() string {
	if dataDir := os.Getenv("DATA_DIR"); dataDir != "" {
		return dataDir
	}
	return "./testdata"
}

// ensureDataPath 确保数据路径存在，如果是相对路径则自动构建到 cayley 子目录
func ensureDataPath(path string) (string, error) {
	// 如果路径包含路径分隔符（绝对路径或相对路径），直接使用
	if strings.Contains(path, string(filepath.Separator)) || strings.Contains(path, "/") || strings.Contains(path, "\\") {
		// 确保目录存在
		dir := filepath.Dir(path)
		if dir != "." && dir != "" {
			if err := ensureDir(dir); err != nil {
				return "", fmt.Errorf("failed to create directory: %w", err)
			}
		}
		return path, nil
	}

	// 如果是相对路径（不包含路径分隔符），自动构建到 cayley 子目录
	dataDir := getDataDir()
	fullPath := filepath.Join(dataDir, "cayley", path)

	// 确保目录存在
	dir := filepath.Dir(fullPath)
	if err := ensureDir(dir); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	return fullPath, nil
}

// NewGraph 创建新的图数据库实例
// path: SQLite3 数据库文件路径
// - 完整路径：/path/to/graph.db 或 ./path/to/graph.db
// - 相对路径（如 "graph.db"）：自动构建到 {DATA_DIR}/cayley/ 目录
func NewGraph(path string) (Graph, error) {
	// 自动构建路径并创建目录
	finalPath, err := ensureDataPath(path)
	if err != nil {
		return nil, err
	}

	// 打开数据库连接
	// 使用 modernc.org/sqlite 驱动，通过连接字符串参数设置 PRAGMA
	dsn := finalPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// PRAGMA 已通过连接字符串参数设置，无需再次执行

	graph := &cayleyGraph{db: db}

	// 初始化数据库表结构
	ctx := context.Background()
	if err := graph.initSchema(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return graph, nil
}

// initSchema 初始化数据库表结构
func (g *cayleyGraph) initSchema(ctx context.Context) error {
	// 创建三元组表
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS quads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		subject TEXT NOT NULL,
		predicate TEXT NOT NULL,
		object TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		UNIQUE(subject, predicate, object)
	);

	CREATE INDEX IF NOT EXISTS idx_quads_subject ON quads(subject);
	CREATE INDEX IF NOT EXISTS idx_quads_predicate ON quads(predicate);
	CREATE INDEX IF NOT EXISTS idx_quads_object ON quads(object);
	CREATE INDEX IF NOT EXISTS idx_quads_sp ON quads(subject, predicate);
	CREATE INDEX IF NOT EXISTS idx_quads_po ON quads(predicate, object);
	`

	_, err := g.db.ExecContext(ctx, createTableSQL)
	return err
}

// Link 创建一条边
func (g *cayleyGraph) Link(ctx context.Context, subject, predicate, object string) error {
	query := `INSERT OR IGNORE INTO quads (subject, predicate, object) VALUES (?, ?, ?)`

	// 重试逻辑：处理 SQLITE_BUSY 错误
	maxRetries := 5
	var err error
	for i := 0; i < maxRetries; i++ {
		_, err = g.db.ExecContext(ctx, query, subject, predicate, object)
		if err == nil {
			return nil
		}

		// 检查是否是 SQLITE_BUSY 错误
		errStr := err.Error()
		if strings.Contains(errStr, "database is locked") || strings.Contains(errStr, "SQLITE_BUSY") {
			// 指数退避：等待时间逐渐增加
			waitTime := time.Duration(i+1) * 10 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		}

		// 如果不是 SQLITE_BUSY 错误，直接返回
		return err
	}

	return err
}

// Unlink 删除一条边
func (g *cayleyGraph) Unlink(ctx context.Context, subject, predicate, object string) error {
	query := `DELETE FROM quads WHERE subject = ? AND predicate = ? AND object = ?`

	// 重试逻辑：处理 SQLITE_BUSY 错误
	maxRetries := 5
	var err error
	for i := 0; i < maxRetries; i++ {
		_, err = g.db.ExecContext(ctx, query, subject, predicate, object)
		if err == nil {
			return nil
		}

		// 检查是否是 SQLITE_BUSY 错误
		errStr := err.Error()
		if strings.Contains(errStr, "database is locked") || strings.Contains(errStr, "SQLITE_BUSY") {
			// 指数退避：等待时间逐渐增加
			waitTime := time.Duration(i+1) * 10 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		}

		// 如果不是 SQLITE_BUSY 错误，直接返回
		return err
	}

	return err
}

// GetNeighbors 获取指定节点的邻居节点（出边）
func (g *cayleyGraph) GetNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	var rows *sql.Rows
	var err error

	if predicate == "" {
		query := `SELECT DISTINCT object FROM quads WHERE subject = ?`
		rows, err = g.db.QueryContext(ctx, query, node)
	} else {
		query := `SELECT DISTINCT object FROM quads WHERE subject = ? AND predicate = ?`
		rows, err = g.db.QueryContext(ctx, query, node, predicate)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var neighbors []string
	for rows.Next() {
		var obj string
		if err := rows.Scan(&obj); err != nil {
			return nil, err
		}
		neighbors = append(neighbors, obj)
	}

	return neighbors, rows.Err()
}

// GetInNeighbors 获取指向指定节点的邻居节点（入边）
func (g *cayleyGraph) GetInNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	var rows *sql.Rows
	var err error

	if predicate == "" {
		query := `SELECT DISTINCT subject FROM quads WHERE object = ?`
		rows, err = g.db.QueryContext(ctx, query, node)
	} else {
		query := `SELECT DISTINCT subject FROM quads WHERE object = ? AND predicate = ?`
		rows, err = g.db.QueryContext(ctx, query, node, predicate)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var neighbors []string
	for rows.Next() {
		var subj string
		if err := rows.Scan(&subj); err != nil {
			return nil, err
		}
		neighbors = append(neighbors, subj)
	}

	return neighbors, rows.Err()
}

// AllTriples 获取图中所有的三元组
func (g *cayleyGraph) AllTriples(ctx context.Context) ([]Triple, error) {
	query := `SELECT subject, predicate, object FROM quads`
	rows, err := g.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var triples []Triple
	for rows.Next() {
		var t Triple
		if err := rows.Scan(&t.Subject, &t.Predicate, &t.Object); err != nil {
			return nil, err
		}
		triples = append(triples, t)
	}
	return triples, rows.Err()
}

// Query 返回查询构建器
func (g *cayleyGraph) Query() GraphQuery {
	return &graphQuery{graph: g}
}

// FindPath 查找从 from 到 to 的路径（使用 BFS）
func (g *cayleyGraph) FindPath(ctx context.Context, from, to string, maxDepth int, predicate string) ([][]string, error) {
	if maxDepth <= 0 {
		maxDepth = 10 // 默认最大深度
	}

	type pathNode struct {
		node string
		path []string
	}

	queue := []pathNode{{node: from, path: []string{from}}}
	visited := make(map[string]bool)
	visited[from] = true
	var paths [][]string

	for len(queue) > 0 && len(paths) < 100 { // 限制返回的路径数量
		current := queue[0]
		queue = queue[1:]

		if len(current.path) > maxDepth {
			continue
		}

		if current.node == to {
			paths = append(paths, current.path)
			continue
		}

		// 获取邻居节点
		var neighbors []string
		var err error
		if predicate == "" {
			neighbors, err = g.GetNeighbors(ctx, current.node, "")
		} else {
			neighbors, err = g.GetNeighbors(ctx, current.node, predicate)
		}
		if err != nil {
			return nil, err
		}

		for _, neighbor := range neighbors {
			// 避免循环
			if visited[neighbor] {
				continue
			}

			newPath := make([]string, len(current.path))
			copy(newPath, current.path)
			newPath = append(newPath, neighbor)

			visited[neighbor] = true
			queue = append(queue, pathNode{node: neighbor, path: newPath})
		}
	}

	return paths, nil
}

// Close 关闭数据库连接
func (g *cayleyGraph) Close() error {
	return g.db.Close()
}

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
				outQuery := `SELECT predicate, object FROM quads WHERE subject = ?`
				if step.predicate != "" {
					outQuery = `SELECT predicate, object FROM quads WHERE subject = ? AND predicate = ?`
				}
				var outRows *sql.Rows
				var err error
				if step.predicate != "" {
					outRows, err = q.graph.db.QueryContext(ctx, outQuery, node, step.predicate)
				} else {
					outRows, err = q.graph.db.QueryContext(ctx, outQuery, node)
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
				inQuery := `SELECT subject, predicate FROM quads WHERE object = ?`
				if step.predicate != "" {
					inQuery = `SELECT subject, predicate FROM quads WHERE object = ? AND predicate = ?`
				}
				var inRows *sql.Rows
				if step.predicate != "" {
					inRows, err = q.graph.db.QueryContext(ctx, inQuery, node, step.predicate)
				} else {
					inRows, err = q.graph.db.QueryContext(ctx, inQuery, node)
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

// ensureDir 确保目录存在
func ensureDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}
