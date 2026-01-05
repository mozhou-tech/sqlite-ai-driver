package lightrag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	sqlite3_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

// --- Interfaces ---

// Database 定义数据库接口
type Database interface {
	// Collection 获取或创建集合
	Collection(ctx context.Context, name string, schema Schema) (Collection, error)
	// Graph 获取图数据库实例
	Graph() GraphDatabase
	// Close 关闭数据库连接
	Close(ctx context.Context) error
}

// Schema 定义集合的schema
type Schema struct {
	PrimaryKey string
	RevField   string
}

// Collection 定义文档集合接口
type Collection interface {
	// Insert 插入文档
	Insert(ctx context.Context, doc map[string]any) (Document, error)
	// FindByID 根据ID查找文档
	FindByID(ctx context.Context, id string) (Document, error)
	// Find 根据选项查找文档
	Find(ctx context.Context, opts FindOptions) ([]Document, error)
	// Delete 根据ID删除文档
	Delete(ctx context.Context, id string) error
	// BulkUpsert 批量插入或更新文档
	BulkUpsert(ctx context.Context, docs []map[string]any) ([]Document, error)
}

// FindOptions 查找选项
type FindOptions struct {
	Limit    int
	Offset   int
	Selector map[string]any
}

// Document 定义文档接口
type Document interface {
	// ID 返回文档ID
	ID() string
	// Data 返回文档数据
	Data() map[string]any
}

// FulltextSearch 定义全文搜索接口
type FulltextSearch interface {
	// FindWithScores 执行全文搜索并返回带分数的结果
	FindWithScores(ctx context.Context, query string, opts FulltextSearchOptions) ([]FulltextSearchResult, error)
	// Close 关闭全文搜索资源
	Close() error
}

// FulltextSearchOptions 全文搜索选项
type FulltextSearchOptions struct {
	Limit    int
	Selector map[string]any
}

// FulltextSearchResult 全文搜索结果
type FulltextSearchResult struct {
	Document Document
	Score    float64
}

// VectorSearch 定义向量搜索接口
type VectorSearch interface {
	// Search 执行向量搜索
	Search(ctx context.Context, embedding []float64, opts VectorSearchOptions) ([]VectorSearchResult, error)
	// Close 关闭向量搜索资源
	Close() error
}

// VectorSearchOptions 向量搜索选项
type VectorSearchOptions struct {
	Limit    int
	Selector map[string]any
}

// VectorSearchResult 向量搜索结果
type VectorSearchResult struct {
	Document Document
	Score    float64
}

// GraphDatabase 定义图数据库接口
type GraphDatabase interface {
	// Link 创建一条从 subject 到 object 的边，边的类型为 predicate
	Link(ctx context.Context, subject, predicate, object string) error
	// GetNeighbors 获取从 node 出发的邻居节点 (Out-neighbors)
	GetNeighbors(ctx context.Context, node, predicate string) ([]string, error)
	// GetInNeighbors 获取指向 node 的邻居节点 (In-neighbors)
	GetInNeighbors(ctx context.Context, node, predicate string) ([]string, error)
	// AllTriples 获取所有三元组
	AllTriples(ctx context.Context) ([]GraphQueryResult, error)
	// Query 返回查询构建器
	Query() GraphQuery
}

// GraphQuery 定义图查询构建器接口
type GraphQuery interface {
	// V 选择指定的节点
	V(node string) GraphQuery
	// Both 获取双向邻居
	Both() GraphQuery
	// In 获取入向邻居
	In(predicate string) GraphQuery
	// Out 获取出向邻居
	Out(predicate string) GraphQuery
	// All 执行查询并返回所有结果
	All(ctx context.Context) ([]GraphQueryResult, error)
}

// GraphQueryResult 图查询结果
type GraphQueryResult struct {
	Subject   string
	Predicate string
	Object    string
}

// DatabaseOptions 数据库选项
type DatabaseOptions struct {
	Name         string
	WorkingDir   string // 工作目录，作为基础目录
	GraphOptions *GraphOptions
}

// GraphOptions 图数据库选项
type GraphOptions struct {
	Enabled bool
	Backend string
}

// FulltextSearchConfig 全文搜索配置
type FulltextSearchConfig struct {
	Identifier  string
	DocToString func(doc map[string]any) string
}

// VectorSearchConfig 向量搜索配置
type VectorSearchConfig struct {
	Identifier     string
	DocToEmbedding func(doc map[string]any) ([]float64, error)
	Dimensions     int
}

// --- SQLite Implementation ---

// DocumentModel GORM 模型，表示文档集合中的文档
type DocumentModel struct {
	ID              string    `gorm:"type:VARCHAR;primaryKey;not null"`
	Content         string    `gorm:"type:TEXT"`
	Metadata        string    `gorm:"type:TEXT"`                       // JSON 格式的元数据
	ContentTokens   string    `gorm:"type:TEXT;column:content_tokens"` // 分词后的内容，用于 FTS
	EmbeddingStatus string    `gorm:"type:VARCHAR;default:'pending';column:embedding_status"`
	ChunkLength     int       `gorm:"type:INTEGER;column:chunk_length"`
	Rev             int       `gorm:"column:_rev;default:1"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`
}

// sqliteDatabase 基于SQLite的数据库实现
type sqliteDatabase struct {
	db          *gorm.DB
	graph       cayley_driver.Graph
	collections []*sqliteCollection // 跟踪所有创建的集合，以便在关闭时停止它们的 worker
	mu          sync.Mutex          // 保护 collections 的并发访问
}

// CreateDatabase 创建数据库实例
// 注意：数据库路径会被 sqlite3-driver 统一映射到共享数据库文件 {WorkingDir}/db/index.db
// 目录创建由 sqlite3-driver 自动处理，无需在此处创建
//
// 数据库文件行为：
// - 如果数据库文件已存在：会打开现有数据库，保留所有现有数据和表结构
// - 如果数据库文件不存在：SQLite 会自动创建新的数据库文件
// - 表创建：使用 GORM AutoMigrate，如果表已存在则不会重新创建
// - 列添加：GORM 会自动处理列的增加（向后兼容）
func CreateDatabase(ctx context.Context, opts DatabaseOptions) (Database, error) {
	// 构建数据库路径
	// sqlite3-driver 会自动处理路径映射，如果提供了 workingDir，会映射到 {workingDir}/db/index.db
	dbPath := "index.db"
	dsn := dbPath
	if opts.WorkingDir != "" {
		dsn = fmt.Sprintf("%s?workingDir=%s", dbPath, opts.WorkingDir)
	}

	// 使用 sqlite3-driver 提供的 dialector 打开数据库
	// sqlite3-driver 会自动处理路径映射和 WAL 模式
	db, err := gorm.Open(sqlite3_driver.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 初始化图数据库（如果需要）
	var graph cayley_driver.Graph
	if opts.GraphOptions != nil && opts.GraphOptions.Enabled {
		if opts.WorkingDir == "" {
			sqlDB, _ := db.DB()
			if sqlDB != nil {
				sqlDB.Close()
			}
			return nil, fmt.Errorf("WorkingDir is required when GraphOptions.Enabled is true")
		}
		// 使用 graphstore 约定的数据库文件路径 "graphstore.db"
		// cayley-driver 会自动将其映射到 {workingDir}/graph/graphstore.db
		// 使用表前缀 "lightrag_" 以区分不同的数据
		graph, err = cayley_driver.NewGraphWithNamespace(opts.WorkingDir, cayley_driver.GRAPH_DB_FILE, "lightrag_")
		if err != nil {
			sqlDB, _ := db.DB()
			if sqlDB != nil {
				sqlDB.Close()
			}
			return nil, fmt.Errorf("failed to create graph database: %w", err)
		}
	}

	return &sqliteDatabase{
		db:    db,
		graph: graph,
	}, nil
}

func (d *sqliteDatabase) Collection(ctx context.Context, name string, schema Schema) (Collection, error) {
	tableName := name

	// 使用 GORM AutoMigrate 创建表（如果不存在）
	// 使用 GORM 的 Table 方法指定表名进行迁移
	err := d.db.Table(tableName).AutoMigrate(&DocumentModel{})
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	collection := &sqliteCollection{
		db:        d.db,
		tableName: tableName,
		schema:    schema,
	}

	// 将集合添加到数据库的跟踪列表中
	d.mu.Lock()
	d.collections = append(d.collections, collection)
	d.mu.Unlock()

	return collection, nil
}

// getEmbeddingLimiter 获取或初始化 embedding 速率限制器（每秒5次）
func (c *sqliteCollection) getEmbeddingLimiter() *rate.Limiter {
	c.limiterOnce.Do(func() {
		// 每秒5次，burst 为1（严格限制，不允许突发）
		// rate.Limit(5) 表示每秒5次 = 每200ms一次
		// burst 1 表示令牌桶中最多有1个令牌，每次请求消耗1个令牌
		// 这样确保严格按每秒5次的速率执行，不允许突发
		c.embeddingLimiter = rate.NewLimiter(rate.Limit(5), 1)
		logrus.WithFields(logrus.Fields{
			"rate":  "5 per second",
			"burst": 1,
		}).Info("Embedding rate limiter initialized")
	})
	return c.embeddingLimiter
}

func (d *sqliteDatabase) Graph() GraphDatabase {
	if d.graph == nil {
		return nil
	}
	return &sqliteGraphDatabase{graph: d.graph}
}

func (d *sqliteDatabase) Close(ctx context.Context) error {
	// 在关闭数据库之前，停止所有集合的后台 worker
	d.mu.Lock()
	for _, collection := range d.collections {
		collection.stopEmbeddingWorker()
	}
	d.mu.Unlock()

	var errs []error
	if d.db != nil {
		sqlDB, err := d.db.DB()
		if err == nil && sqlDB != nil {
			if err := sqlDB.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if d.graph != nil {
		if err := d.graph.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing database: %v", errs)
	}
	return nil
}

// sqliteCollection 基于SQLite的集合实现
type sqliteCollection struct {
	db               *gorm.DB
	tableName        string
	schema           Schema
	vectorSearches   []*sqliteVectorSearch // 存储所有注册的向量搜索配置
	embeddingLimiter *rate.Limiter         // Embedding API 速率限制器（每秒5次）
	limiterOnce      sync.Once             // 确保 limiter 只初始化一次

	// 后台 embedding worker 相关字段
	embeddingWorkerCtx    context.Context
	embeddingWorkerCancel context.CancelFunc
	embeddingWorkerWg     sync.WaitGroup
	embeddingWorkerOnce   sync.Once
}

func (c *sqliteCollection) Insert(ctx context.Context, doc map[string]any) (Document, error) {
	id, ok := doc["id"].(string)
	if !ok {
		return nil, fmt.Errorf("document must have 'id' field")
	}

	content, _ := doc["content"].(string)

	// 如果chunk不超过10个字符，则不需要嵌入和入库存储
	chunkLength := len([]rune(content))
	if chunkLength <= 10 {
		logrus.WithFields(logrus.Fields{
			"id":          id,
			"content_len": chunkLength,
		}).Debug("Skipping chunk that is too short (<=10 characters)")
		return nil, nil
	}

	// 构建metadata（排除id和content）
	metadata := make(map[string]any)
	for k, v := range doc {
		if k != "id" && k != "content" && k != "_rev" {
			metadata[k] = v
		}
	}
	metadataJSON, _ := json.Marshal(metadata)

	// 使用 sego 进行分词
	tokens := ""
	if content != "" {
		tokens = sego.Tokenize(content)
	}

	// 使用 GORM 进行插入或更新
	model := &DocumentModel{
		ID:              id,
		Content:         content,
		Metadata:        string(metadataJSON),
		ContentTokens:   tokens,
		EmbeddingStatus: "pending",
		ChunkLength:     chunkLength,
		Rev:             1,
	}

	// 使用 GORM 的 Save 方法（如果主键存在则更新，否则插入）
	result := c.db.Table(c.tableName).WithContext(ctx).Save(model)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to insert document: %w", result.Error)
	}

	// 如果是更新，需要增加 _rev
	if result.RowsAffected > 0 {
		c.db.Table(c.tableName).WithContext(ctx).
			Where("id = ?", id).
			Update("_rev", gorm.Expr("_rev + 1"))
	}

	// 启动后台 embedding worker（如果还没有启动）
	c.startEmbeddingWorker(ctx)

	return &sqliteDocument{
		id:      id,
		data:    doc,
		content: content,
	}, nil
}

func (c *sqliteCollection) FindByID(ctx context.Context, id string) (Document, error) {
	var model DocumentModel
	err := c.db.Table(c.tableName).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find document: %w", err)
	}

	doc := map[string]any{
		"id":      model.ID,
		"content": model.Content,
	}

	if model.Metadata != "" {
		var metadata map[string]any
		if err := json.Unmarshal([]byte(model.Metadata), &metadata); err == nil {
			for k, val := range metadata {
				doc[k] = val
			}
		}
	}

	return &sqliteDocument{
		id:      model.ID,
		data:    doc,
		content: model.Content,
	}, nil
}

func (c *sqliteCollection) Find(ctx context.Context, opts FindOptions) ([]Document, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := opts.Offset

	var models []DocumentModel
	query := c.db.Table(c.tableName).WithContext(ctx)

	// TODO: 实现 Selector 过滤

	query = query.Order("created_at DESC").Limit(limit).Offset(offset)

	err := query.Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find documents: %w", err)
	}

	var results []Document
	for _, model := range models {
		doc := map[string]any{
			"id":      model.ID,
			"content": model.Content,
		}

		if model.Metadata != "" {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(model.Metadata), &metadata); err == nil {
				for k, val := range metadata {
					doc[k] = val
				}
			}
		}

		results = append(results, &sqliteDocument{
			id:      model.ID,
			data:    doc,
			content: model.Content,
		})
	}

	return results, nil
}

func (c *sqliteCollection) Delete(ctx context.Context, id string) error {
	err := c.db.Table(c.tableName).WithContext(ctx).Where("id = ?", id).Delete(&DocumentModel{}).Error
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}
	return nil
}

func (c *sqliteCollection) BulkUpsert(ctx context.Context, docs []map[string]any) ([]Document, error) {
	if len(docs) == 0 {
		return []Document{}, nil
	}

	var results []Document
	var models []DocumentModel

	for _, doc := range docs {
		id, ok := doc["id"].(string)
		if !ok {
			return nil, fmt.Errorf("document must have 'id' field")
		}

		content, _ := doc["content"].(string)

		// 如果chunk不超过10个字符，则跳过该文档
		chunkLength := len([]rune(content))
		if chunkLength <= 10 {
			logrus.WithFields(logrus.Fields{
				"id":          id,
				"content_len": chunkLength,
			}).Debug("Skipping chunk that is too short (<=10 characters)")
			continue
		}

		metadata := make(map[string]any)
		for k, v := range doc {
			if k != "id" && k != "content" && k != "_rev" {
				metadata[k] = v
			}
		}
		metadataJSON, _ := json.Marshal(metadata)

		// 使用 sego 进行分词
		tokens := ""
		if content != "" {
			tokens = sego.Tokenize(content)
		}

		model := DocumentModel{
			ID:              id,
			Content:         content,
			Metadata:        string(metadataJSON),
			ContentTokens:   tokens,
			EmbeddingStatus: "pending",
			ChunkLength:     chunkLength,
			Rev:             1,
		}
		models = append(models, model)

		results = append(results, &sqliteDocument{
			id:      id,
			data:    doc,
			content: content,
		})
	}

	// 使用 GORM 批量保存
	if len(models) > 0 {
		err := c.db.Table(c.tableName).WithContext(ctx).Save(&models).Error
		if err != nil {
			return nil, fmt.Errorf("failed to upsert documents: %w", err)
		}

		// 更新 _rev 字段（对于已存在的记录）
		for _, model := range models {
			c.db.Table(c.tableName).WithContext(ctx).
				Where("id = ?", model.ID).
				Update("_rev", gorm.Expr("_rev + 1"))
		}
	}

	// 启动后台 embedding worker（如果还没有启动）
	c.startEmbeddingWorker(ctx)

	logrus.WithFields(logrus.Fields{
		"total_docs": len(docs),
	}).Info("Documents inserted, embeddings will be processed asynchronously")

	return results, nil
}

// sqliteDocument 文档实现
type sqliteDocument struct {
	id      string
	data    map[string]any
	content string
}

func (d *sqliteDocument) ID() string {
	return d.id
}

func (d *sqliteDocument) Data() map[string]any {
	return d.data
}

// sqliteFulltextSearch 全文搜索实现
type sqliteFulltextSearch struct {
	db        *gorm.DB
	tableName string
	config    FulltextSearchConfig
}

func AddFulltextSearch(collection Collection, config FulltextSearchConfig) (FulltextSearch, error) {
	// 类型断言获取底层实现
	sqliteColl, ok := collection.(*sqliteCollection)
	if !ok {
		return nil, fmt.Errorf("collection is not a sqlite collection")
	}

	// 创建 SQLite FTS5 虚拟表
	ftsTableName := sqliteColl.tableName + "_fts"
	createFTSSQL := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(
			id UNINDEXED,
			content,
			content_tokens,
			content_rowid=id
		)
	`, ftsTableName)

	err := sqliteColl.db.Exec(createFTSSQL).Error
	if err != nil {
		// 如果表已存在，忽略错误
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "duplicate") {
			return nil, fmt.Errorf("failed to create FTS table: %w", err)
		}
	}

	// 创建触发器以同步数据到 FTS 表
	triggerSQL := fmt.Sprintf(`
		CREATE TRIGGER IF NOT EXISTS %s_fts_insert AFTER INSERT ON %s BEGIN
			INSERT INTO %s(rowid, content, content_tokens) VALUES (new.id, new.content, new.content_tokens);
		END;
		CREATE TRIGGER IF NOT EXISTS %s_fts_update AFTER UPDATE ON %s BEGIN
			UPDATE %s SET content = new.content, content_tokens = new.content_tokens WHERE rowid = new.id;
		END;
		CREATE TRIGGER IF NOT EXISTS %s_fts_delete AFTER DELETE ON %s BEGIN
			DELETE FROM %s WHERE rowid = old.id;
		END;
	`, sqliteColl.tableName, sqliteColl.tableName, ftsTableName,
		sqliteColl.tableName, sqliteColl.tableName, ftsTableName,
		sqliteColl.tableName, sqliteColl.tableName, ftsTableName)

	_ = sqliteColl.db.Exec(triggerSQL).Error

	// 更新所有已存在文档的 content_tokens（如果它们还没有被填充）
	var models []DocumentModel
	sqliteColl.db.Table(sqliteColl.tableName).
		Where("content IS NOT NULL AND (content_tokens IS NULL OR content_tokens = '')").
		Find(&models)

	for _, model := range models {
		if model.Content != "" {
			tokens := sego.Tokenize(model.Content)
			sqliteColl.db.Table(sqliteColl.tableName).
				Where("id = ?", model.ID).
				Update("content_tokens", tokens)
		}
	}

	// 同步现有数据到 FTS 表
	syncSQL := fmt.Sprintf(`
		INSERT OR REPLACE INTO %s(rowid, content, content_tokens)
		SELECT id, content, content_tokens FROM %s WHERE content IS NOT NULL
	`, ftsTableName, sqliteColl.tableName)
	_ = sqliteColl.db.Exec(syncSQL).Error

	return &sqliteFulltextSearch{
		db:        sqliteColl.db,
		tableName: sqliteColl.tableName,
		config:    config,
	}, nil
}

func (f *sqliteFulltextSearch) FindWithScores(ctx context.Context, query string, opts FulltextSearchOptions) ([]FulltextSearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	// 使用 sego 分词查询
	queryTokens := sego.Tokenize(query)
	if queryTokens == "" {
		// 如果分词结果为空，使用原始查询
		queryTokens = query
	}
	ftsTableName := f.tableName + "_fts"

	// 使用 SQLite FTS5 进行搜索
	// FTS5 MATCH 查询需要使用 bm25() 函数来获取 rank
	// 注意：如果 FTS5 表为空或查询无结果，Rows() 可能返回 nil
	searchSQL := fmt.Sprintf(`
		SELECT 
			d.id,
			d.content,
			d.metadata,
			bm25(%s) as rank
		FROM %s fts
		JOIN %s d ON fts.rowid = d.id
		WHERE %s MATCH ?
		ORDER BY rank
		LIMIT ?
	`, ftsTableName, ftsTableName, f.tableName, ftsTableName)

	var results []FulltextSearchResult

	// 使用 WithContext 确保查询在正确的上下文中执行
	stmt := f.db.WithContext(ctx).Raw(searchSQL, queryTokens, limit*2)
	if stmt.Error != nil {
		return nil, fmt.Errorf("failed to prepare search: %w", stmt.Error)
	}

	rows, err := stmt.Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to execute search: %w", err)
	}
	if rows == nil {
		// 如果 rows 为 nil，可能是查询没有结果或 FTS5 表不存在
		// 返回空结果而不是错误
		return results, nil
	}
	defer rows.Close()

	var models []struct {
		ID       string
		Content  string
		Metadata string
		Rank     float64
	}

	for rows.Next() {
		var model struct {
			ID       string
			Content  string
			Metadata string
			Rank     float64
		}
		if err := rows.Scan(&model.ID, &model.Content, &model.Metadata, &model.Rank); err != nil {
			continue
		}
		models = append(models, model)
	}

	for _, model := range models {
		doc := map[string]any{
			"id":      model.ID,
			"content": model.Content,
		}

		if model.Metadata != "" {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(model.Metadata), &metadata); err == nil {
				for k, val := range metadata {
					doc[k] = val
				}
			}
		}

		// 应用 Selector 过滤器
		if opts.Selector != nil && len(opts.Selector) > 0 {
			matched := true
			for key, expectedValue := range opts.Selector {
				actualValue, exists := doc[key]
				if !exists || actualValue != expectedValue {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
		}

		// 使用 FTS5 的 rank 分数，归一化到 0-1 范围
		score := 1.0 / (1.0 + model.Rank)

		results = append(results, FulltextSearchResult{
			Document: &sqliteDocument{
				id:      model.ID,
				data:    doc,
				content: model.Content,
			},
			Score: score,
		})

		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

func (f *sqliteFulltextSearch) Close() error {
	// SQLite FTS5 表不需要显式关闭
	return nil
}

// sqliteVectorSearch 向量搜索实现
type sqliteVectorSearch struct {
	db        *gorm.DB
	tableName string
	config    VectorSearchConfig
}

func AddVectorSearch(collection Collection, config VectorSearchConfig) (VectorSearch, error) {
	sqliteColl, ok := collection.(*sqliteCollection)
	if !ok {
		return nil, fmt.Errorf("collection is not a sqlite collection")
	}

	// 检查并创建vector列（SQLite 使用 TEXT 存储 JSON 格式的向量数组）
	vectorColumn := "vector_" + config.Identifier
	// 使用 GORM 检查列是否存在（通过尝试查询）
	var count int64
	sqliteColl.db.Table(sqliteColl.tableName).
		Select("COUNT(*)").Where("1=0").Count(&count) // 先检查表是否存在

	// 尝试添加列（如果不存在，GORM 会自动处理）
	// SQLite 不支持直接检查列是否存在，我们使用 ALTER TABLE IF NOT EXISTS 的变通方法
	// 由于 SQLite 的限制，我们直接尝试添加列，如果已存在会失败但可以忽略
	alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT`, sqliteColl.tableName, vectorColumn)
	_ = sqliteColl.db.Exec(alterTableSQL).Error // 忽略错误，列可能已存在

	vectorSearch := &sqliteVectorSearch{
		db:        sqliteColl.db,
		tableName: sqliteColl.tableName,
		config:    config,
	}

	// 注册向量搜索到集合中，以便在插入时自动计算向量
	sqliteColl.vectorSearches = append(sqliteColl.vectorSearches, vectorSearch)

	// 启动后台 embedding worker（如果还没有启动）
	sqliteColl.startEmbeddingWorker(context.Background())

	return vectorSearch, nil
}

func (v *sqliteVectorSearch) Search(ctx context.Context, embedding []float64, opts VectorSearchOptions) ([]VectorSearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	vectorColumn := "vector_" + v.config.Identifier

	if len(embedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	}

	// SQLite 使用 TEXT 存储 JSON 格式的向量数组
	// 我们需要查询所有候选文档，然后在应用层计算余弦相似度
	// 查询所有 embedding_status = 'completed' 的文档
	var models []DocumentModel
	err := v.db.Table(v.tableName).WithContext(ctx).
		Where(fmt.Sprintf("%s IS NOT NULL AND embedding_status = ?", vectorColumn), "completed").
		Find(&models).Error
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"table_name":    v.tableName,
			"vector_column": vectorColumn,
		}).Error("Vector search query failed")
		return nil, fmt.Errorf("failed to search vectors: %w", err)
	}

	// 在内存中计算余弦相似度并排序
	type candidateResult struct {
		model  DocumentModel
		score  float64
		vector []float64
	}

	var candidates []candidateResult
	for _, model := range models {
		// 从 JSON 字符串解析向量
		var storedVector []float64
		// 尝试从 vectorColumn 字段读取向量
		// 由于 GORM 模型中没有这个动态字段，我们需要使用 Raw SQL 查询
		var vectorJSON string
		querySQL := fmt.Sprintf("SELECT %s FROM %s WHERE id = ?", vectorColumn, v.tableName)
		err := v.db.Raw(querySQL, model.ID).Scan(&vectorJSON).Error
		if err != nil || vectorJSON == "" {
			continue
		}

		if err := json.Unmarshal([]byte(vectorJSON), &storedVector); err != nil {
			continue
		}

		if len(storedVector) != len(embedding) {
			continue
		}

		// 计算余弦相似度
		score := cosineSimilarity(embedding, storedVector)
		candidates = append(candidates, candidateResult{
			model:  model,
			score:  score,
			vector: storedVector,
		})
	}

	// 按分数排序
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[i].score < candidates[j].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// 限制结果数量
	if len(candidates) > limit*2 {
		candidates = candidates[:limit*2]
	}

	var results []VectorSearchResult
	for _, candidate := range candidates {
		doc := map[string]any{
			"id":      candidate.model.ID,
			"content": candidate.model.Content,
		}

		if candidate.model.Metadata != "" {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(candidate.model.Metadata), &metadata); err == nil {
				for k, val := range metadata {
					doc[k] = val
				}
			}
		}

		// 应用 Selector 过滤器
		if opts.Selector != nil && len(opts.Selector) > 0 {
			matched := true
			for key, expectedValue := range opts.Selector {
				actualValue, exists := doc[key]
				if !exists || actualValue != expectedValue {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
		}

		results = append(results, VectorSearchResult{
			Document: &sqliteDocument{
				id:      candidate.model.ID,
				data:    doc,
				content: candidate.model.Content,
			},
			Score: candidate.score,
		})

		if len(results) >= limit {
			break
		}
	}

	logrus.WithFields(logrus.Fields{
		"table_name":       v.tableName,
		"vector_column":    vectorColumn,
		"total_candidates": len(candidates),
		"filtered_results": len(results),
		"limit":            limit,
	}).Info("Vector search completed")

	return results, nil
}

func (v *sqliteVectorSearch) Close() error {
	return nil
}

// sqliteGraphDatabase 图数据库实现
type sqliteGraphDatabase struct {
	graph cayley_driver.Graph
}

func (g *sqliteGraphDatabase) Link(ctx context.Context, subject, predicate, object string) error {
	return g.graph.Link(ctx, subject, predicate, object)
}

func (g *sqliteGraphDatabase) GetNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	return g.graph.GetNeighbors(ctx, node, predicate)
}

func (g *sqliteGraphDatabase) GetInNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	return g.graph.GetInNeighbors(ctx, node, predicate)
}

func (g *sqliteGraphDatabase) AllTriples(ctx context.Context) ([]GraphQueryResult, error) {
	triples, err := g.graph.AllTriples(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]GraphQueryResult, 0, len(triples))
	for _, t := range triples {
		results = append(results, GraphQueryResult{
			Subject:   t.Subject,
			Predicate: t.Predicate,
			Object:    t.Object,
		})
	}
	return results, nil
}

func (g *sqliteGraphDatabase) Query() GraphQuery {
	return &sqliteGraphQuery{graph: g.graph}
}

// sqliteGraphQuery 图查询实现
type sqliteGraphQuery struct {
	graph     cayley_driver.Graph
	startNode string
	steps     []queryStep
}

type queryStep struct {
	direction string // "out", "in", "both"
	predicate string
}

func (q *sqliteGraphQuery) V(node string) GraphQuery {
	return &sqliteGraphQuery{
		graph:     q.graph,
		startNode: node,
		steps:     q.steps,
	}
}

func (q *sqliteGraphQuery) Both() GraphQuery {
	return &sqliteGraphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "both",
			predicate: "",
		}),
	}
}

func (q *sqliteGraphQuery) In(predicate string) GraphQuery {
	return &sqliteGraphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "in",
			predicate: predicate,
		}),
	}
}

func (q *sqliteGraphQuery) Out(predicate string) GraphQuery {
	return &sqliteGraphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "out",
			predicate: predicate,
		}),
	}
}

func (q *sqliteGraphQuery) All(ctx context.Context) ([]GraphQueryResult, error) {
	if q.startNode == "" {
		return nil, fmt.Errorf("query must start with V(node)")
	}

	if len(q.steps) == 0 {
		return []GraphQueryResult{}, nil
	}

	var results []GraphQueryResult
	currentNodes := []string{q.startNode}

	for i, step := range q.steps {
		var nextNodes []string
		var triples []cayley_driver.Triple

		for _, node := range currentNodes {
			if step.direction == "both" {
				// 使用 cayley_driver.GraphQuery 的 Both() 方法来获取所有关系（包括predicate）
				cayleyQuery := q.graph.Query().V(node)
				// 使用 Both() 方法获取所有双向关系
				allTriples, err := cayleyQuery.Both().All(ctx)
				if err == nil {
					triples = append(triples, allTriples...)
					for _, t := range allTriples {
						// 确定目标节点
						target := t.Object
						if target == node {
							target = t.Subject
						}
						nextNodes = append(nextNodes, target)
					}
				}
			} else if step.direction == "out" {
				neighbors, _ := q.graph.GetNeighbors(ctx, node, step.predicate)
				for _, neighbor := range neighbors {
					triples = append(triples, cayley_driver.Triple{
						Subject:   node,
						Predicate: step.predicate,
						Object:    neighbor,
					})
					nextNodes = append(nextNodes, neighbor)
				}
			} else if step.direction == "in" {
				neighbors, _ := q.graph.GetInNeighbors(ctx, node, step.predicate)
				for _, neighbor := range neighbors {
					triples = append(triples, cayley_driver.Triple{
						Subject:   neighbor,
						Predicate: step.predicate,
						Object:    node,
					})
					nextNodes = append(nextNodes, neighbor)
				}
			}

			// 如果是最后一步，收集所有三元组
			if i == len(q.steps)-1 {
				for _, t := range triples {
					results = append(results, GraphQueryResult{
						Subject:   t.Subject,
						Predicate: t.Predicate,
						Object:    t.Object,
					})
				}
			}
		}

		currentNodes = nextNodes
	}

	return results, nil
}

// startEmbeddingWorker 启动后台 embedding worker（只启动一次）
func (c *sqliteCollection) startEmbeddingWorker(ctx context.Context) {
	c.embeddingWorkerOnce.Do(func() {
		workerCtx, cancel := context.WithCancel(context.Background())
		c.embeddingWorkerCtx = workerCtx
		c.embeddingWorkerCancel = cancel

		c.embeddingWorkerWg.Add(1)
		go c.embeddingWorker(workerCtx)
		logrus.Info("Background embedding worker started")
	})
}

// stopEmbeddingWorker 停止后台 embedding worker
func (c *sqliteCollection) stopEmbeddingWorker() {
	if c.embeddingWorkerCancel != nil {
		c.embeddingWorkerCancel()
		c.embeddingWorkerWg.Wait()
		logrus.Info("Background embedding worker stopped")
	}
}

// embeddingWorker 后台 worker，定期检查并处理 pending 状态的 embedding
func (c *sqliteCollection) embeddingWorker(ctx context.Context) {
	defer c.embeddingWorkerWg.Done()

	ticker := time.NewTicker(2 * time.Second) // 每2秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.processPendingEmbeddings(ctx)
		}
	}
}

// processPendingEmbeddings 处理所有 pending 状态的 embedding
func (c *sqliteCollection) processPendingEmbeddings(ctx context.Context) {
	if len(c.vectorSearches) == 0 {
		return
	}

	// 使用独立的 context，避免使用可能被取消的请求 context
	processCtx := context.Background()

	// 查询所有 pending 状态的文档，限制每次处理的数量（并发处理100个）
	var models []DocumentModel
	err := c.db.Table(c.tableName).WithContext(processCtx).
		Where("embedding_status = ?", "pending").
		Limit(100).
		Find(&models).Error
	if err != nil {
		// 如果数据库已关闭，这是预期的行为，不需要记录错误
		sqlDB, _ := c.db.DB()
		if sqlDB != nil {
			if err.Error() == "sql: database is closed" {
				return
			}
		}
		logrus.WithError(err).Error("Failed to query pending embeddings")
		return
	}

	var pendingDocs []struct {
		id       string
		content  string
		metadata string
	}

	for _, model := range models {
		pendingDocs = append(pendingDocs, struct {
			id       string
			content  string
			metadata string
		}{id: model.ID, content: model.Content, metadata: model.Metadata})
	}

	if len(pendingDocs) == 0 {
		return
	}

	logrus.WithField("count", len(pendingDocs)).Info("Processing pending embeddings concurrently")

	// 使用 errgroup 并发处理所有文档
	g, gCtx := errgroup.WithContext(processCtx)

	// 限制并发数量，避免过多并发导致资源耗尽
	// 使用 semaphore 模式控制并发数
	sem := make(chan struct{}, 100) // 最多100个并发

	for _, doc := range pendingDocs {
		doc := doc // 避免闭包问题

		g.Go(func() error {
			// 获取信号量
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-gCtx.Done():
				return gCtx.Err()
			}

			// 检查 worker context 是否已取消
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// 将状态更新为 processing
			result := c.db.Table(c.tableName).WithContext(processCtx).
				Where("id = ? AND embedding_status = ?", doc.id, "pending").
				Update("embedding_status", "processing")
			if result.Error != nil {
				logrus.WithError(result.Error).WithField("doc_id", doc.id).Error("Failed to update embedding status to processing")
				return nil // 不返回错误，继续处理其他文档
			}

			// 检查是否成功更新（可能被其他 worker 处理了）
			if result.RowsAffected == 0 {
				return nil // 文档已被其他 worker 处理
			}

			// 如果chunk不超过10个字符，则跳过嵌入处理
			if len([]rune(doc.content)) <= 10 {
				logrus.WithFields(logrus.Fields{
					"doc_id":      doc.id,
					"content_len": len([]rune(doc.content)),
				}).Debug("Skipping embedding for chunk that is too short (<=10 characters)")
				// 直接标记为 completed，跳过嵌入
				err = c.db.Table(c.tableName).WithContext(processCtx).
					Where("id = ?", doc.id).
					Update("embedding_status", "completed").Error
				if err != nil {
					logrus.WithError(err).WithField("doc_id", doc.id).Error("Failed to update embedding status to completed")
				}
				return nil
			}

			// 解析 metadata
			var metadataMap map[string]any
			if doc.metadata != "" {
				if err := json.Unmarshal([]byte(doc.metadata), &metadataMap); err != nil {
					metadataMap = make(map[string]any)
				}
			} else {
				metadataMap = make(map[string]any)
			}

			// 构建文档对象
			docMap := map[string]any{
				"id":       doc.id,
				"content":  doc.content,
				"metadata": metadataMap,
			}
			for k, v := range metadataMap {
				docMap[k] = v
			}

			// 为每个向量搜索配置生成 embedding
			allSuccess := true
			for _, vs := range c.vectorSearches {
				if vs.config.DocToEmbedding == nil {
					continue
				}

				// 等待速率限制器允许（每秒最多5次）
				limiter := c.getEmbeddingLimiter()
				if err := limiter.Wait(processCtx); err != nil {
					logrus.WithError(err).WithFields(logrus.Fields{
						"doc_id":      doc.id,
						"content_len": len(doc.content),
					}).Error("Rate limiter wait failed")
					allSuccess = false
					continue
				}

				// 生成 embedding（DocToEmbedding 内部会使用 context.Background()，避免 context canceled 错误）
				embedding, err := vs.config.DocToEmbedding(docMap)
				if err != nil {
					// 检查是否是 context canceled 错误
					if err == context.Canceled || err == context.DeadlineExceeded {
						logrus.WithError(err).WithFields(logrus.Fields{
							"doc_id":      doc.id,
							"content_len": len(doc.content),
							"note":        "This should not happen as we use context.Background()",
						}).Warn("Embedding failed due to context cancellation (unexpected)")
					} else {
						logrus.WithError(err).WithFields(logrus.Fields{
							"doc_id":      doc.id,
							"content_len": len(doc.content),
							"source":      "background_worker",
						}).Error("Failed to generate embedding in background worker")
					}
					allSuccess = false
					continue
				}

				if len(embedding) > 0 {
					// 转换为 JSON 格式存储
					vectorJSON, err := json.Marshal(embedding)
					if err != nil {
						logrus.WithError(err).WithField("doc_id", doc.id).Error("Failed to marshal embedding")
						allSuccess = false
						continue
					}
					vectorColumn := "vector_" + vs.config.Identifier
					err = c.db.Table(c.tableName).WithContext(processCtx).
						Where("id = ?", doc.id).
						Update(vectorColumn, string(vectorJSON)).Error
					if err != nil {
						logrus.WithError(err).WithFields(logrus.Fields{
							"doc_id":        doc.id,
							"vector_column": vectorColumn,
						}).Error("Failed to update vector column")
						allSuccess = false
					} else {
						logrus.WithFields(logrus.Fields{
							"doc_id":      doc.id,
							"vector_dim":  len(embedding),
							"content_len": len(doc.content),
						}).Debug("Successfully generated and stored embedding")
					}
				} else {
					logrus.WithField("doc_id", doc.id).Warn("Empty embedding vector generated")
					allSuccess = false
				}
			}

			// 更新状态
			status := "completed"
			if !allSuccess {
				status = "failed"
			}
			err = c.db.Table(c.tableName).WithContext(processCtx).
				Where("id = ?", doc.id).
				Update("embedding_status", status).Error
			if err != nil {
				logrus.WithError(err).WithField("doc_id", doc.id).Error("Failed to update embedding status")
			}
			return nil
		})
	}

	// 等待所有并发任务完成
	if err := g.Wait(); err != nil {
		logrus.WithError(err).Error("Error processing pending embeddings")
	}
}

// countPendingEmbeddings 统计 pending 或 processing 状态的嵌入数量
func (c *sqliteCollection) countPendingEmbeddings(ctx context.Context) (int, error) {
	if len(c.vectorSearches) == 0 {
		return 0, nil
	}

	var count int64
	err := c.db.Table(c.tableName).WithContext(ctx).
		Where("embedding_status IN ?", []string{"pending", "processing"}).
		Count(&count).Error
	if err != nil {
		// 如果数据库已关闭，这是预期的行为
		sqlDB, _ := c.db.DB()
		if sqlDB != nil {
			if err.Error() == "sql: database is closed" {
				return 0, nil
			}
		}
		return 0, fmt.Errorf("failed to count pending embeddings: %w", err)
	}

	return int(count), nil
}

// cosineSimilarity 计算两个向量的余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0.0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
