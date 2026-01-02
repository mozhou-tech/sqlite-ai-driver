package graphsearch

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
)

// graphsearch 基于cayley-driver和sqlite-driver的纯图谱存储
// - 使用cayley-driver存储图谱关系（三元组）
// - 使用sqlite-driver的index.db共享数据库存储实体的embedding信息，用于向量检索
// - 支持语义检索图谱（通过向量相似度搜索找到相关实体，然后返回图谱关系）
type graphsearch struct {
	graph       cayley_driver.Graph
	db          *sql.DB
	embedder    Embedder
	tableName   string
	initialized bool
	mu          sync.Mutex
}

// New 创建graphsearch实例
func New(opts Options) (*graphsearch, error) {
	tableName := opts.TableName
	if tableName == "" {
		tableName = "graphsearch_entities"
	}

	workingDir := opts.WorkingDir
	if workingDir == "" {
		return nil, fmt.Errorf("WorkingDir is required")
	}

	// 创建图谱数据库（使用 graphsearch_ 表前缀）
	graph, err := cayley_driver.NewGraphWithNamespace(workingDir, cayley_driver.GRAPH_DB_FILE, "graphsearch_")
	if err != nil {
		return nil, fmt.Errorf("failed to create graph: %w", err)
	}

	return &graphsearch{
		graph:     graph,
		embedder:  opts.Embedder,
		tableName: tableName,
	}, nil
}

// Initialize 初始化存储后端
func (g *graphsearch) Initialize(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.initialized {
		return nil
	}

	// 打开DuckDB数据库用于向量检索
	// 注意：无论传入什么路径，都会被 sqlite-driver 统一映射到共享数据库文件 ./data/indexing/index.db
	// 向量检索使用 sqlite-driver 的 index.db 共享数据库，不同的业务模块通过表名区分
	db, err := sql.Open("duckdb", "graphsearch.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	g.db = db

	// 创建表（如果不存在）
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			entity_id VARCHAR PRIMARY KEY,
			entity_name TEXT,
			metadata JSON,
			embedding FLOAT[],
			embedding_status VARCHAR DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`, g.tableName)

	_, err = g.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// 创建索引
	createIndexSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_entity_name ON %s(entity_name);
		CREATE INDEX IF NOT EXISTS idx_%s_embedding_status ON %s(embedding_status);
	`, g.tableName, g.tableName, g.tableName, g.tableName)

	_, err = g.db.ExecContext(ctx, createIndexSQL)
	if err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	g.initialized = true
	return nil
}

// Close 关闭graphsearch
func (g *graphsearch) Close() error {
	var errs []error

	if g.graph != nil {
		if err := g.graph.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if g.db != nil {
		if err := g.db.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing graphsearch: %v", errs)
	}

	return nil
}
