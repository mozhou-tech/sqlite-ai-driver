package cayley_driver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// cayleyGraph 是基于 SQLite3 的图数据库实现
type cayleyGraph struct {
	db          *sql.DB
	tablePrefix string
}

// NewGraphWithNamespace 创建新的图数据库实例（支持表命名空间）
// workingDir: 工作目录，作为基础目录，相对路径会构建到 {workingDir}/data.db
// path: SQLite3 数据库文件路径
//   - 完整路径：/path/to/data.db 或 ./path/to/data.db
//   - 相对路径（如 "data.db"）：自动构建到 {workingDir}/data.db（与 sqlite3-driver 共用同一数据库文件）
//
// namespace: 表命名空间，如果为空则使用默认的 "quads" 表名
func NewGraphWithNamespace(workingDir, path, namespace string) (Graph, error) {
	// 自动构建路径并创建目录
	finalPath, err := ensureDataPath(workingDir, path)
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

	graph := &cayleyGraph{
		db:          db,
		tablePrefix: namespace,
	}

	// 初始化数据库表结构
	ctx := context.Background()
	if err := graph.initSchema(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return graph, nil
}

// tableName 获取完整的表名（带前缀）
func (g *cayleyGraph) tableName() string {
	if g.tablePrefix != "" {
		return g.tablePrefix + "quads"
	}
	return "quads"
}

// getDB 获取数据库连接（供 query 使用）
func (g *cayleyGraph) getDB() *sql.DB {
	return g.db
}

// initSchema 初始化数据库表结构
func (g *cayleyGraph) initSchema(ctx context.Context) error {
	tableName := g.tableName()
	// 创建三元组表
	createTableSQL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		subject TEXT NOT NULL,
		predicate TEXT NOT NULL,
		object TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%%s', 'now')),
		UNIQUE(subject, predicate, object)
	);

	CREATE INDEX IF NOT EXISTS idx_%s_subject ON %s(subject);
	CREATE INDEX IF NOT EXISTS idx_%s_predicate ON %s(predicate);
	CREATE INDEX IF NOT EXISTS idx_%s_object ON %s(object);
	CREATE INDEX IF NOT EXISTS idx_%s_sp ON %s(subject, predicate);
	CREATE INDEX IF NOT EXISTS idx_%s_po ON %s(predicate, object);
	`, tableName, tableName, tableName, tableName, tableName, tableName, tableName, tableName, tableName, tableName, tableName)

	_, err := g.db.ExecContext(ctx, createTableSQL)
	return err
}

// Link 创建一条边
func (g *cayleyGraph) Link(ctx context.Context, subject, predicate, object string) error {
	query := fmt.Sprintf(`INSERT OR IGNORE INTO %s (subject, predicate, object) VALUES (?, ?, ?)`, g.tableName())

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
	query := fmt.Sprintf(`DELETE FROM %s WHERE subject = ? AND predicate = ? AND object = ?`, g.tableName())

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
	tableName := g.tableName()

	if predicate == "" {
		query := fmt.Sprintf(`SELECT DISTINCT object FROM %s WHERE subject = ?`, tableName)
		rows, err = g.db.QueryContext(ctx, query, node)
	} else {
		query := fmt.Sprintf(`SELECT DISTINCT object FROM %s WHERE subject = ? AND predicate = ?`, tableName)
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
	tableName := g.tableName()

	if predicate == "" {
		query := fmt.Sprintf(`SELECT DISTINCT subject FROM %s WHERE object = ?`, tableName)
		rows, err = g.db.QueryContext(ctx, query, node)
	} else {
		query := fmt.Sprintf(`SELECT DISTINCT subject FROM %s WHERE object = ? AND predicate = ?`, tableName)
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
	query := fmt.Sprintf(`SELECT subject, predicate, object FROM %s`, g.tableName())
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
