package graphsearch

import (
	"context"
	"fmt"
	"sync"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// graphsearch 基于cayley-driver和sqlite-driver的纯图谱存储
// - 使用cayley-driver存储图谱关系（三元组）
// - 使用sqlite-driver的data.db共享数据库存储实体的embedding信息，用于向量检索
// - 支持语义检索图谱（通过向量相似度搜索找到相关实体，然后返回图谱关系）
type graphsearch struct {
	graph       cayley_driver.Graph
	db          *gorm.DB
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

	// 打开SQLite数据库用于向量检索，使用 GORM
	// 注意：无论传入什么路径，都会被 sqlite-driver 统一映射到共享数据库文件 ./data/indexing/data.db
	// 向量检索使用 sqlite-driver 的 data.db 共享数据库，不同的业务模块通过表名区分
	db, err := gorm.Open(sqlite.Open("graphsearch.db"), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	g.db = db

	// 使用 GORM AutoMigrate 创建表（如果不存在）
	// 注意：由于表名是动态的，我们需要使用 Table() 方法指定表名
	// 但 AutoMigrate 需要模型类型，所以我们先创建一个临时模型
	entityModel := &Entity{}

	// 使用 Table() 方法指定表名进行迁移
	if err := g.db.Table(g.tableName).AutoMigrate(entityModel); err != nil {
		return fmt.Errorf("failed to migrate table: %w", err)
	}

	// 创建索引
	createIndexSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_entity_name ON %s(entity_name);
		CREATE INDEX IF NOT EXISTS idx_%s_embedding_status ON %s(embedding_status);
	`, g.tableName, g.tableName, g.tableName, g.tableName)

	if err := g.db.Exec(createIndexSQL).Error; err != nil {
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
		sqlDB, err := g.db.DB()
		if err == nil {
			if err := sqlDB.Close(); err != nil {
				errs = append(errs, err)
			}
		} else {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing graphsearch: %v", errs)
	}

	return nil
}
