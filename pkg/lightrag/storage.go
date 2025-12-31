package lightrag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	duckdb_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
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
	Path         string
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

// --- DuckDB Implementation ---

// duckdbDatabase 基于DuckDB的数据库实现
type duckdbDatabase struct {
	db          *sql.DB
	graph       cayley_driver.Graph
	path        string
	collections []*duckdbCollection // 跟踪所有创建的集合，以便在关闭时停止它们的 worker
	mu          sync.Mutex          // 保护 collections 的并发访问
}

// CreateDatabase 创建数据库实例
func CreateDatabase(ctx context.Context, opts DatabaseOptions) (Database, error) {
	if opts.Path == "" {
		opts.Path = "./lightrag"
	}

	// 确保目录存在
	dir := filepath.Dir(opts.Path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// 打开DuckDB数据库
	db, err := sql.Open("duckdb", opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 初始化图数据库（如果需要）
	var graph cayley_driver.Graph
	if opts.GraphOptions != nil && opts.GraphOptions.Enabled {
		graphPath := opts.Path + ".graph.db"
		graph, err = cayley_driver.NewGraph(graphPath)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to create graph database: %w", err)
		}
	}

	return &duckdbDatabase{
		db:    db,
		graph: graph,
		path:  opts.Path,
	}, nil
}

func (d *duckdbDatabase) Collection(ctx context.Context, name string, schema Schema) (Collection, error) {
	// 创建表（如果不存在）
	tableName := name
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR PRIMARY KEY,
			content TEXT,
			metadata JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			_rev INTEGER DEFAULT 1
		)
	`, tableName)

	_, err := d.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	// 创建FTS索引（如果不存在）
	// 注意：这里先不创建，等AddFulltextSearch时再创建
	// 创建tokens列用于FTS
	tokensColumn := "content_tokens"
	// 使用 DuckDB 原生的 information_schema 查询列信息，避免触发 sqlite 扩展的 catalog 错误
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM information_schema.columns 
		WHERE table_name = '%s' AND column_name = ?
	`, tableName)

	var count int
	err = d.db.QueryRowContext(ctx, checkColumnSQL, tokensColumn).Scan(&count)
	if err == nil && count == 0 {
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT`, tableName, tokensColumn)
		_, _ = d.db.ExecContext(ctx, alterTableSQL)
	}

	// 创建 embedding_status 列用于跟踪 embedding 状态
	// 状态值: 'pending' (待处理), 'processing' (处理中), 'completed' (已完成), 'failed' (失败)
	statusColumn := "embedding_status"
	err = d.db.QueryRowContext(ctx, checkColumnSQL, statusColumn).Scan(&count)
	if err == nil && count == 0 {
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s VARCHAR DEFAULT 'pending'`, tableName, statusColumn)
		_, _ = d.db.ExecContext(ctx, alterTableSQL)
	}

	// 创建 chunk_length 列用于存储 chunk 长度
	chunkLengthColumn := "chunk_length"
	err = d.db.QueryRowContext(ctx, checkColumnSQL, chunkLengthColumn).Scan(&count)
	if err == nil && count == 0 {
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s INTEGER`, tableName, chunkLengthColumn)
		_, _ = d.db.ExecContext(ctx, alterTableSQL)
	}

	collection := &duckdbCollection{
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
func (c *duckdbCollection) getEmbeddingLimiter() *rate.Limiter {
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

func (d *duckdbDatabase) Graph() GraphDatabase {
	if d.graph == nil {
		return nil
	}
	return &duckdbGraphDatabase{graph: d.graph}
}

func (d *duckdbDatabase) Close(ctx context.Context) error {
	// 在关闭数据库之前，停止所有集合的后台 worker
	d.mu.Lock()
	for _, collection := range d.collections {
		collection.stopEmbeddingWorker()
	}
	d.mu.Unlock()

	var errs []error
	if d.db != nil {
		if err := d.db.Close(); err != nil {
			errs = append(errs, err)
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

// duckdbCollection 基于DuckDB的集合实现
type duckdbCollection struct {
	db               *sql.DB
	tableName        string
	schema           Schema
	vectorSearches   []*duckdbVectorSearch // 存储所有注册的向量搜索配置
	embeddingLimiter *rate.Limiter         // Embedding API 速率限制器（每秒5次）
	limiterOnce      sync.Once             // 确保 limiter 只初始化一次

	// 后台 embedding worker 相关字段
	embeddingWorkerCtx    context.Context
	embeddingWorkerCancel context.CancelFunc
	embeddingWorkerWg     sync.WaitGroup
	embeddingWorkerOnce   sync.Once
}

func (c *duckdbCollection) Insert(ctx context.Context, doc map[string]any) (Document, error) {
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

	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (id, content, metadata, _rev, embedding_status, chunk_length)
		VALUES (?, ?, ?::JSON, 1, 'pending', ?)
		ON CONFLICT (id) DO UPDATE SET
			content = EXCLUDED.content,
			metadata = EXCLUDED.metadata,
			_rev = %s._rev + 1,
			embedding_status = 'pending',
			chunk_length = EXCLUDED.chunk_length
	`, c.tableName, c.tableName)

	_, err := c.db.ExecContext(ctx, insertSQL, id, content, string(metadataJSON), chunkLength)
	if err != nil {
		return nil, fmt.Errorf("failed to insert document: %w", err)
	}

	// 更新tokens列
	if content != "" {
		tokens := duckdb_driver.TokenizeWithSego(content)
		logrus.WithFields(logrus.Fields{
			"id":     id,
			"tokens": tokens,
		}).Debug("Updating content_tokens for document")
		updateSQL := fmt.Sprintf(`UPDATE %s SET content_tokens = ? WHERE id = ?`, c.tableName)
		_, err = c.db.ExecContext(ctx, updateSQL, tokens, id)
		if err != nil {
			// 记录错误但不中断插入流程
			logrus.WithError(err).Warnf("Failed to update content_tokens for document %s", id)
		}
	}

	// 启动后台 embedding worker（如果还没有启动）
	c.startEmbeddingWorker(ctx)

	// 不再立即处理 embedding，而是标记为 pending，由后台 worker 异步处理

	return &duckdbDocument{
		id:      id,
		data:    doc,
		content: content,
	}, nil
}

func (c *duckdbCollection) FindByID(ctx context.Context, id string) (Document, error) {
	selectSQL := fmt.Sprintf(`
		SELECT id, content, metadata
		FROM %s
		WHERE id = ?
	`, c.tableName)

	var docID, content string
	var metadataVal any
	err := c.db.QueryRowContext(ctx, selectSQL, id).Scan(&docID, &content, &metadataVal)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find document: %w", err)
	}

	doc := map[string]any{
		"id":      docID,
		"content": content,
	}

	if metadataVal != nil {
		switch v := metadataVal.(type) {
		case string:
			var metadata map[string]any
			if err := json.Unmarshal([]byte(v), &metadata); err == nil {
				for k, val := range metadata {
					doc[k] = val
				}
			}
		case []byte:
			var metadata map[string]any
			if err := json.Unmarshal(v, &metadata); err == nil {
				for k, val := range metadata {
					doc[k] = val
				}
			}
		case map[string]any:
			for k, val := range v {
				doc[k] = val
			}
		}
	}

	return &duckdbDocument{
		id:      docID,
		data:    doc,
		content: content,
	}, nil
}

func (c *duckdbCollection) Find(ctx context.Context, opts FindOptions) ([]Document, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := opts.Offset

	selectSQL := fmt.Sprintf(`
		SELECT id, content, metadata
		FROM %s
	`, c.tableName)

	// TODO: 实现 Selector 过滤

	selectSQL += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d OFFSET %d", limit, offset)

	rows, err := c.db.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to find documents: %w", err)
	}
	defer rows.Close()

	var results []Document
	for rows.Next() {
		var docID, content string
		var metadataVal any
		if err := rows.Scan(&docID, &content, &metadataVal); err != nil {
			continue
		}

		doc := map[string]any{
			"id":      docID,
			"content": content,
		}

		if metadataVal != nil {
			switch v := metadataVal.(type) {
			case string:
				var metadata map[string]any
				if err := json.Unmarshal([]byte(v), &metadata); err == nil {
					for k, val := range metadata {
						doc[k] = val
					}
				}
			case []byte:
				var metadata map[string]any
				if err := json.Unmarshal(v, &metadata); err == nil {
					for k, val := range metadata {
						doc[k] = val
					}
				}
			case map[string]any:
				for k, val := range v {
					doc[k] = val
				}
			}
		}

		results = append(results, &duckdbDocument{
			id:      docID,
			data:    doc,
			content: content,
		})
	}

	return results, nil
}

func (c *duckdbCollection) Delete(ctx context.Context, id string) error {
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE id = ?", c.tableName)
	_, err := c.db.ExecContext(ctx, deleteSQL, id)
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}
	return nil
}

func (c *duckdbCollection) BulkUpsert(ctx context.Context, docs []map[string]any) ([]Document, error) {
	if len(docs) == 0 {
		return []Document{}, nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var results []Document
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

		// 不使用 PrepareContext，直接使用 ExecContext
		insertSQL := fmt.Sprintf(`
			INSERT INTO %s (id, content, metadata, _rev, embedding_status, chunk_length)
			VALUES (?, ?, ?::JSON, 1, 'pending', ?)
			ON CONFLICT (id) DO UPDATE SET
				content = EXCLUDED.content,
				metadata = EXCLUDED.metadata,
				_rev = %s._rev + 1,
				embedding_status = 'pending',
				chunk_length = EXCLUDED.chunk_length
		`, c.tableName, c.tableName)

		_, err := tx.ExecContext(ctx, insertSQL, id, content, string(metadataJSON), chunkLength)
		if err != nil {
			return nil, fmt.Errorf("failed to upsert document: %w", err)
		}

		// 更新tokens列
		if content != "" {
			tokens := duckdb_driver.TokenizeWithSego(content)
			updateSQL := fmt.Sprintf(`UPDATE %s SET content_tokens = ? WHERE id = ?`, c.tableName)
			_, _ = tx.ExecContext(ctx, updateSQL, tokens, id)
		}

		// 不再立即处理 embedding，而是标记为 pending，由后台 worker 异步处理

		results = append(results, &duckdbDocument{
			id:      id,
			data:    doc,
			content: content,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 启动后台 embedding worker（如果还没有启动）
	c.startEmbeddingWorker(ctx)

	logrus.WithFields(logrus.Fields{
		"total_docs": len(docs),
	}).Info("Documents inserted, embeddings will be processed asynchronously")

	return results, nil
}

// duckdbDocument 文档实现
type duckdbDocument struct {
	id      string
	data    map[string]any
	content string
}

func (d *duckdbDocument) ID() string {
	return d.id
}

func (d *duckdbDocument) Data() map[string]any {
	return d.data
}

// duckdbFulltextSearch 全文搜索实现
type duckdbFulltextSearch struct {
	db        *sql.DB
	tableName string
	config    FulltextSearchConfig
}

func AddFulltextSearch(collection Collection, config FulltextSearchConfig) (FulltextSearch, error) {
	// 类型断言获取底层实现
	duckdbColl, ok := collection.(*duckdbCollection)
	if !ok {
		return nil, fmt.Errorf("collection is not a duckdb collection")
	}

	// 创建FTS索引
	err := duckdb_driver.CreateFTSIndexWithSego(
		context.Background(),
		duckdbColl.db,
		duckdbColl.tableName,
		"id",
		"content",
		"content_tokens",
	)
	if err != nil {
		// 如果索引已存在，忽略错误
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "duplicate") {
			return nil, fmt.Errorf("failed to create FTS index: %w", err)
		}
	}

	// 更新所有已存在文档的 content_tokens（如果它们还没有被填充）
	// 这确保 FTS 索引能够正确索引所有文档
	// 获取所有需要更新的文档
	selectSQL := fmt.Sprintf(`
		SELECT id, content 
		FROM %s 
		WHERE content IS NOT NULL AND (content_tokens IS NULL OR content_tokens = '')
	`, duckdbColl.tableName)

	rows, err := duckdbColl.db.QueryContext(context.Background(), selectSQL)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, content string
			if err := rows.Scan(&id, &content); err == nil && content != "" {
				tokens := duckdb_driver.TokenizeWithSego(content)
				updateSQL := fmt.Sprintf(`UPDATE %s SET content_tokens = ? WHERE id = ?`, duckdbColl.tableName)
				_, _ = duckdbColl.db.ExecContext(context.Background(), updateSQL, tokens, id)
			}
		}
	}

	return &duckdbFulltextSearch{
		db:        duckdbColl.db,
		tableName: duckdbColl.tableName,
		config:    config,
	}, nil
}

func (f *duckdbFulltextSearch) FindWithScores(ctx context.Context, query string, opts FulltextSearchOptions) ([]FulltextSearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	// 使用sego分词搜索
	ids, err := duckdb_driver.SearchWithSego(ctx, f.db, f.tableName, query, "content", "content_tokens", limit*2) // 获取更多结果以便过滤
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}

	var results []FulltextSearchResult
	for i, id := range ids {
		// 获取文档
		selectSQL := fmt.Sprintf(`SELECT id, content, metadata FROM %s WHERE id = ?`, f.tableName)
		var docID, content string
		var metadataVal any
		err := f.db.QueryRowContext(ctx, selectSQL, id).Scan(&docID, &content, &metadataVal)
		if err != nil {
			continue
		}

		doc := map[string]any{
			"id":      docID,
			"content": content,
		}

		if metadataVal != nil {
			switch v := metadataVal.(type) {
			case string:
				var metadata map[string]any
				if err := json.Unmarshal([]byte(v), &metadata); err == nil {
					for k, val := range metadata {
						doc[k] = val
					}
				}
			case []byte:
				var metadata map[string]any
				if err := json.Unmarshal(v, &metadata); err == nil {
					for k, val := range metadata {
						doc[k] = val
					}
				}
			case map[string]any:
				for k, val := range v {
					doc[k] = val
				}
			}
		}

		// 应用 Selector 过滤器
		if opts.Selector != nil && len(opts.Selector) > 0 {
			matched := true
			for key, expectedValue := range opts.Selector {
				// 检查 metadata 中的值
				actualValue, exists := doc[key]
				if !exists {
					// 如果 metadata 中没有，检查是否在顶层 doc 中
					actualValue, exists = doc[key]
				}
				if !exists || actualValue != expectedValue {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
		}

		// 简单的分数计算（基于位置，越靠前分数越高）
		score := 1.0 / float64(i+1)

		results = append(results, FulltextSearchResult{
			Document: &duckdbDocument{
				id:      docID,
				data:    doc,
				content: content,
			},
			Score: score,
		})

		// 如果已经达到限制，停止
		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

func (f *duckdbFulltextSearch) Close() error {
	// DuckDB的FTS索引不需要显式关闭
	return nil
}

// duckdbVectorSearch 向量搜索实现
type duckdbVectorSearch struct {
	db        *sql.DB
	tableName string
	config    VectorSearchConfig
}

func AddVectorSearch(collection Collection, config VectorSearchConfig) (VectorSearch, error) {
	duckdbColl, ok := collection.(*duckdbCollection)
	if !ok {
		return nil, fmt.Errorf("collection is not a duckdb collection")
	}

	// 检查并创建vector列
	vectorColumn := "vector_" + config.Identifier
	// 使用 DuckDB 原生的 information_schema 查询列信息，避免触发 sqlite 扩展的 catalog 错误
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM information_schema.columns 
		WHERE table_name = '%s' AND column_name = ?
	`, duckdbColl.tableName)

	var count int
	err := duckdbColl.db.QueryRowContext(context.Background(), checkColumnSQL, vectorColumn).Scan(&count)
	if err == nil && count == 0 {
		// 创建vector列
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s FLOAT[]`, duckdbColl.tableName, vectorColumn)
		_, err = duckdbColl.db.ExecContext(context.Background(), alterTableSQL)
		if err != nil {
			return nil, fmt.Errorf("failed to add vector column: %w", err)
		}
	}

	vectorSearch := &duckdbVectorSearch{
		db:        duckdbColl.db,
		tableName: duckdbColl.tableName,
		config:    config,
	}

	// 注册向量搜索到集合中，以便在插入时自动计算向量
	duckdbColl.vectorSearches = append(duckdbColl.vectorSearches, vectorSearch)

	// 启动后台 embedding worker（如果还没有启动）
	duckdbColl.startEmbeddingWorker(context.Background())

	return vectorSearch, nil
}

func (v *duckdbVectorSearch) Search(ctx context.Context, embedding []float64, opts VectorSearchOptions) ([]VectorSearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	vectorColumn := "vector_" + v.config.Identifier

	// Convert []float64 to string format that DuckDB can parse
	// DuckDB requires FLOAT[] type, but go-duckdb driver doesn't support []float64 directly
	// So we convert to string format and use CAST in SQL
	var vectorArg interface{}
	if len(embedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	} else {
		// Convert []float64 to string format that DuckDB can parse
		// Format: [1.0, 2.0, 3.0]
		vectorStr := "["
		for i, v := range embedding {
			if i > 0 {
				vectorStr += ", "
			}
			vectorStr += fmt.Sprintf("%g", v)
		}
		vectorStr += "]"
		vectorArg = vectorStr
	}

	// 使用DuckDB的list_cosine_similarity进行向量搜索
	// 只查询 embedding_status = 'completed' 的文档，确保只返回已成功生成 embedding 的文档
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			1 - list_cosine_similarity(%s, ?::FLOAT[]) as distance
		FROM %s
		WHERE %s IS NOT NULL AND embedding_status = 'completed'
		ORDER BY list_cosine_similarity(%s, ?::FLOAT[]) DESC
		LIMIT ?
	`, vectorColumn, v.tableName, vectorColumn, vectorColumn)

	logrus.WithFields(logrus.Fields{
		"table_name":    v.tableName,
		"vector_column": vectorColumn,
		"limit":         limit * 2,
	}).Debug("Executing vector search query")

	rows, err := v.db.QueryContext(ctx, sqlQuery, vectorArg, vectorArg, limit*2) // 获取更多结果以便过滤
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"table_name":    v.tableName,
			"vector_column": vectorColumn,
		}).Error("Vector search query failed")
		return nil, fmt.Errorf("failed to search vectors: %w", err)
	}
	defer rows.Close()

	var results []VectorSearchResult
	resultCount := 0
	for rows.Next() {
		resultCount++
		var id, content string
		var metadataVal any
		var distance float64

		err := rows.Scan(&id, &content, &metadataVal, &distance)
		if err != nil {
			continue
		}

		doc := map[string]any{
			"id":      id,
			"content": content,
		}

		if metadataVal != nil {
			switch v := metadataVal.(type) {
			case string:
				var metadata map[string]any
				if err := json.Unmarshal([]byte(v), &metadata); err == nil {
					for k, val := range metadata {
						doc[k] = val
					}
				}
			case []byte:
				var metadata map[string]any
				if err := json.Unmarshal(v, &metadata); err == nil {
					for k, val := range metadata {
						doc[k] = val
					}
				}
			case map[string]any:
				for k, val := range v {
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

		// 将distance转换为similarity score
		score := 1.0 - distance

		results = append(results, VectorSearchResult{
			Document: &duckdbDocument{
				id:      id,
				data:    doc,
				content: content,
			},
			Score: score,
		})

		if len(results) >= limit {
			break
		}
	}

	logrus.WithFields(logrus.Fields{
		"table_name":       v.tableName,
		"vector_column":    vectorColumn,
		"total_rows":       resultCount,
		"filtered_results": len(results),
		"limit":            limit,
	}).Info("Vector search completed")

	return results, nil
}

func (v *duckdbVectorSearch) Close() error {
	return nil
}

// duckdbGraphDatabase 图数据库实现
type duckdbGraphDatabase struct {
	graph cayley_driver.Graph
}

func (g *duckdbGraphDatabase) Link(ctx context.Context, subject, predicate, object string) error {
	return g.graph.Link(ctx, subject, predicate, object)
}

func (g *duckdbGraphDatabase) GetNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	return g.graph.GetNeighbors(ctx, node, predicate)
}

func (g *duckdbGraphDatabase) GetInNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	return g.graph.GetInNeighbors(ctx, node, predicate)
}

func (g *duckdbGraphDatabase) AllTriples(ctx context.Context) ([]GraphQueryResult, error) {
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

func (g *duckdbGraphDatabase) Query() GraphQuery {
	return &duckdbGraphQuery{graph: g.graph}
}

// duckdbGraphQuery 图查询实现
type duckdbGraphQuery struct {
	graph     cayley_driver.Graph
	startNode string
	steps     []queryStep
}

type queryStep struct {
	direction string // "out", "in", "both"
	predicate string
}

func (q *duckdbGraphQuery) V(node string) GraphQuery {
	return &duckdbGraphQuery{
		graph:     q.graph,
		startNode: node,
		steps:     q.steps,
	}
}

func (q *duckdbGraphQuery) Both() GraphQuery {
	return &duckdbGraphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "both",
			predicate: "",
		}),
	}
}

func (q *duckdbGraphQuery) In(predicate string) GraphQuery {
	return &duckdbGraphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "in",
			predicate: predicate,
		}),
	}
}

func (q *duckdbGraphQuery) Out(predicate string) GraphQuery {
	return &duckdbGraphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "out",
			predicate: predicate,
		}),
	}
}

func (q *duckdbGraphQuery) All(ctx context.Context) ([]GraphQueryResult, error) {
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
func (c *duckdbCollection) startEmbeddingWorker(ctx context.Context) {
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
func (c *duckdbCollection) stopEmbeddingWorker() {
	if c.embeddingWorkerCancel != nil {
		c.embeddingWorkerCancel()
		c.embeddingWorkerWg.Wait()
		logrus.Info("Background embedding worker stopped")
	}
}

// embeddingWorker 后台 worker，定期检查并处理 pending 状态的 embedding
func (c *duckdbCollection) embeddingWorker(ctx context.Context) {
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
func (c *duckdbCollection) processPendingEmbeddings(ctx context.Context) {
	if len(c.vectorSearches) == 0 {
		return
	}

	// 使用独立的 context，避免使用可能被取消的请求 context
	processCtx := context.Background()

	// 查询所有 pending 状态的文档，限制每次处理的数量
	selectSQL := fmt.Sprintf(`
		SELECT id, content, metadata
		FROM %s
		WHERE embedding_status = 'pending'
		LIMIT 10
	`, c.tableName)

	rows, err := c.db.QueryContext(processCtx, selectSQL)
	if err != nil {
		// 如果数据库已关闭，这是预期的行为，不需要记录错误
		if err.Error() == "sql: database is closed" {
			return
		}
		logrus.WithError(err).Error("Failed to query pending embeddings")
		return
	}
	defer rows.Close()

	var pendingDocs []struct {
		id       string
		content  string
		metadata string
	}

	for rows.Next() {
		var id, content, metadata string
		if err := rows.Scan(&id, &content, &metadata); err != nil {
			continue
		}
		pendingDocs = append(pendingDocs, struct {
			id       string
			content  string
			metadata string
		}{id: id, content: content, metadata: metadata})
	}

	if len(pendingDocs) == 0 {
		return
	}

	logrus.WithField("count", len(pendingDocs)).Debug("Processing pending embeddings")

	// 处理每个文档的 embedding
	for _, doc := range pendingDocs {
		// 检查 worker context 是否已取消
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 将状态更新为 processing
		updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'processing' WHERE id = ? AND embedding_status = 'pending'`, c.tableName)
		result, err := c.db.ExecContext(processCtx, updateStatusSQL, doc.id)
		if err != nil {
			logrus.WithError(err).WithField("doc_id", doc.id).Error("Failed to update embedding status to processing")
			continue
		}

		// 检查是否成功更新（可能被其他 worker 处理了）
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			continue // 文档已被其他 worker 处理
		}

		// 如果chunk不超过10个字符，则跳过嵌入处理
		if len([]rune(doc.content)) <= 10 {
			logrus.WithFields(logrus.Fields{
				"doc_id":      doc.id,
				"content_len": len([]rune(doc.content)),
			}).Debug("Skipping embedding for chunk that is too short (<=10 characters)")
			// 直接标记为 completed，跳过嵌入
			updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'completed' WHERE id = ?`, c.tableName)
			_, err = c.db.ExecContext(processCtx, updateStatusSQL, doc.id)
			if err != nil {
				logrus.WithError(err).WithField("doc_id", doc.id).Error("Failed to update embedding status to completed")
			}
			continue
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
				// 转换为字符串格式
				vectorStr := "["
				for i, v := range embedding {
					if i > 0 {
						vectorStr += ", "
					}
					vectorStr += fmt.Sprintf("%g", v)
				}
				vectorStr += "]"
				vectorColumn := "vector_" + vs.config.Identifier
				updateSQL := fmt.Sprintf(`UPDATE %s SET %s = ?::FLOAT[] WHERE id = ?`, c.tableName, vectorColumn)
				_, err = c.db.ExecContext(processCtx, updateSQL, vectorStr, doc.id)
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
		updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = ? WHERE id = ?`, c.tableName)
		_, err = c.db.ExecContext(processCtx, updateStatusSQL, status, doc.id)
		if err != nil {
			logrus.WithError(err).WithField("doc_id", doc.id).Error("Failed to update embedding status")
		}
	}
}
