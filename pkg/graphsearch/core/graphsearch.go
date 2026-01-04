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
// - 实现完整的 GraphRAG 框架（Query Processor, Retriever, Organizer, Generator）
type graphsearch struct {
	graph       cayley_driver.Graph
	db          *gorm.DB
	embedder    Embedder
	tableName   string
	initialized bool
	mu          sync.Mutex

	// GraphRAG 组件
	queryProcessor QueryProcessor
	retriever      Retriever
	organizer      Organizer
	generator      Generator
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

	gs := &graphsearch{
		graph:     graph,
		embedder:  opts.Embedder,
		tableName: tableName,
	}

	// 初始化 GraphRAG 组件（延迟初始化，需要在 Initialize 之后）
	// 这里先创建引用，实际的组件初始化在 Initialize 之后

	return gs, nil
}

// Initialize 初始化存储后端
func (g *graphsearch) Initialize(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.initialized {
		return nil
	}

	// 打开SQLite数据库用于向量检索，使用 GORM
	// 注意：无论传入什么路径，都会被 sqlite-driver 统一映射到共享数据库文件 ./testdata/indexing/data.db
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

	// 初始化 GraphRAG 组件
	g.queryProcessor = NewDefaultQueryProcessor(g.embedder, g)
	g.retriever = NewDefaultRetriever(g)
	g.organizer = NewDefaultOrganizer(g)
	g.generator = NewDefaultGenerator(g, nil) // 可以传入外部LLM生成器

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

// GraphRAGQuery 执行完整的 GraphRAG 查询流程
// 这是 GraphRAG 框架的主要入口，整合了 Query Processor, Retriever, Organizer, Generator
func (g *graphsearch) GraphRAGQuery(ctx context.Context, query string, options *GraphRAGOptions) (*GeneratedAnswer, error) {
	// 验证输入
	if err := ValidateQuery(query); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	// 验证选项
	if err := ValidateGraphRAGOptions(options); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	// 设置默认选项
	if options == nil {
		options = DefaultGraphRAGOptions()
	}

	// 使用默认组件（如果未提供）
	queryProcessor := options.QueryProcessor
	if queryProcessor == nil {
		queryProcessor = g.queryProcessor
	}

	retriever := options.Retriever
	if retriever == nil {
		retriever = g.retriever
	}

	organizer := options.Organizer
	if organizer == nil {
		organizer = g.organizer
	}

	generator := options.Generator
	if generator == nil {
		generator = g.generator
	}

	// 1. Query Processing: 处理查询
	structuredQuery, err := queryProcessor.ProcessQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query processing failed: %w", err)
	}

	// 2. Retrieval: 检索相关信息
	retrievalOptions := options.RetrievalOptions
	if retrievalOptions == nil {
		retrievalOptions = DefaultRetrievalOptions()
	}

	retrievalResult, err := retriever.Retrieve(ctx, structuredQuery, retrievalOptions)
	if err != nil {
		return nil, fmt.Errorf("retrieval failed: %w", err)
	}

	// 3. Organization: 组织检索结果
	organizationOptions := options.OrganizationOptions
	if organizationOptions == nil {
		organizationOptions = DefaultOrganizationOptions()
	}

	organizedResult, err := organizer.Organize(ctx, retrievalResult, organizationOptions)
	if err != nil {
		return nil, fmt.Errorf("organization failed: %w", err)
	}

	// 4. Generation: 生成最终答案
	generationOptions := options.GenerationOptions
	if generationOptions == nil {
		generationOptions = DefaultGenerationOptions()
	}

	answer, err := generator.Generate(ctx, organizedResult, query, generationOptions)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	return answer, nil
}

// GetQueryProcessor 获取查询处理器（用于自定义）
func (g *graphsearch) GetQueryProcessor() QueryProcessor {
	return g.queryProcessor
}

// GetRetriever 获取检索器（用于自定义）
func (g *graphsearch) GetRetriever() Retriever {
	return g.retriever
}

// GetOrganizer 获取组织器（用于自定义）
func (g *graphsearch) GetOrganizer() Organizer {
	return g.organizer
}

// GetGenerator 获取生成器（用于自定义）
func (g *graphsearch) GetGenerator() Generator {
	return g.generator
}

// SetLLMGenerator 设置LLM生成器
func (g *graphsearch) SetLLMGenerator(llm LLMGenerator) {
	g.generator = NewDefaultGenerator(g, llm)
}
