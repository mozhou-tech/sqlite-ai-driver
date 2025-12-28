package lightrag_driver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	duckdb_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

// duckdbDatabase 基于DuckDB的数据库实现
type duckdbDatabase struct {
	db    *sql.DB
	graph cayley_driver.Graph
	path  string
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
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM pragma_table_info('%s') 
		WHERE name = ?
	`, tableName)

	var count int
	err = d.db.QueryRowContext(ctx, checkColumnSQL, tokensColumn).Scan(&count)
	if err == nil && count == 0 {
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT`, tableName, tokensColumn)
		_, _ = d.db.ExecContext(ctx, alterTableSQL)
	}

	return &duckdbCollection{
		db:        d.db,
		tableName: tableName,
		schema:    schema,
	}, nil
}

func (d *duckdbDatabase) Graph() GraphDatabase {
	if d.graph == nil {
		return nil
	}
	return &duckdbGraphDatabase{graph: d.graph}
}

func (d *duckdbDatabase) Close(ctx context.Context) error {
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
	db        *sql.DB
	tableName string
	schema    Schema
}

func (c *duckdbCollection) Insert(ctx context.Context, doc map[string]any) (Document, error) {
	id, ok := doc["id"].(string)
	if !ok {
		return nil, fmt.Errorf("document must have 'id' field")
	}

	content, _ := doc["content"].(string)
	
	// 构建metadata（排除id和content）
	metadata := make(map[string]any)
	for k, v := range doc {
		if k != "id" && k != "content" && k != "_rev" {
			metadata[k] = v
		}
	}
	metadataJSON, _ := json.Marshal(metadata)

	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (id, content, metadata, _rev)
		VALUES (?, ?, ?, 1)
	`, c.tableName)

	_, err := c.db.ExecContext(ctx, insertSQL, id, content, string(metadataJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to insert document: %w", err)
	}

	// 更新tokens列
	if content != "" {
		tokens := duckdb_driver.TokenizeWithSego(content)
		updateSQL := fmt.Sprintf(`UPDATE %s SET content_tokens = ? WHERE id = ?`, c.tableName)
		_, _ = c.db.ExecContext(ctx, updateSQL, tokens, id)
	}

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
	var metadataJSON sql.NullString
	err := c.db.QueryRowContext(ctx, selectSQL, id).Scan(&docID, &content, &metadataJSON)
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

	if metadataJSON.Valid {
		var metadata map[string]any
		if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err == nil {
			for k, v := range metadata {
				doc[k] = v
			}
		}
	}

	return &duckdbDocument{
		id:      docID,
		data:    doc,
		content: content,
	}, nil
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

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (id, content, metadata, _rev)
		VALUES (?, ?, ?, COALESCE((SELECT _rev FROM %s WHERE id = ?), 0) + 1)
	`, c.tableName, c.tableName))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var results []Document
	for _, doc := range docs {
		id, ok := doc["id"].(string)
		if !ok {
			return nil, fmt.Errorf("document must have 'id' field")
		}

		content, _ := doc["content"].(string)
		
		metadata := make(map[string]any)
		for k, v := range doc {
			if k != "id" && k != "content" && k != "_rev" {
				metadata[k] = v
			}
		}
		metadataJSON, _ := json.Marshal(metadata)

		_, err := stmt.ExecContext(ctx, id, content, string(metadataJSON), id)
		if err != nil {
			return nil, fmt.Errorf("failed to upsert document: %w", err)
		}

		// 更新tokens列
		if content != "" {
			tokens := duckdb_driver.TokenizeWithSego(content)
			updateSQL := fmt.Sprintf(`UPDATE %s SET content_tokens = ? WHERE id = ?`, c.tableName)
			_, _ = tx.ExecContext(ctx, updateSQL, tokens, id)
		}

		results = append(results, &duckdbDocument{
			id:      id,
			data:    doc,
			content: content,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

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
	ids, err := duckdb_driver.SearchWithSego(ctx, f.db, f.tableName, query, "content", "content_tokens", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}

	var results []FulltextSearchResult
	for i, id := range ids {
		// 获取文档
		selectSQL := fmt.Sprintf(`SELECT id, content, metadata FROM %s WHERE id = ?`, f.tableName)
		var docID, content string
		var metadataJSON sql.NullString
		err := f.db.QueryRowContext(ctx, selectSQL, id).Scan(&docID, &content, &metadataJSON)
		if err != nil {
			continue
		}

		doc := map[string]any{
			"id":      docID,
			"content": content,
		}

		if metadataJSON.Valid {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err == nil {
				for k, v := range metadata {
					doc[k] = v
				}
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
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM pragma_table_info('%s') 
		WHERE name = ?
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

	return &duckdbVectorSearch{
		db:        duckdbColl.db,
		tableName: duckdbColl.tableName,
		config:    config,
	}, nil
}

func (v *duckdbVectorSearch) Search(ctx context.Context, embedding []float64, opts VectorSearchOptions) ([]VectorSearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	vectorColumn := "vector_" + v.config.Identifier

	// 使用DuckDB的list_cosine_similarity进行向量搜索
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			1 - list_cosine_similarity(%s, ?::FLOAT[]) as distance
		FROM %s
		WHERE %s IS NOT NULL
		ORDER BY list_cosine_similarity(%s, ?::FLOAT[]) DESC
		LIMIT ?
	`, vectorColumn, v.tableName, vectorColumn, vectorColumn)

	rows, err := v.db.QueryContext(ctx, sqlQuery, embedding, embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search vectors: %w", err)
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var id, content string
		var metadataJSON sql.NullString
		var distance float64

		err := rows.Scan(&id, &content, &metadataJSON, &distance)
		if err != nil {
			continue
		}

		doc := map[string]any{
			"id":      id,
			"content": content,
		}

		if metadataJSON.Valid {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err == nil {
				for k, v := range metadata {
					doc[k] = v
				}
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
	}

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
	// 添加both步骤
	return &duckdbGraphQuery{
		graph:     q.graph,
		startNode: q.startNode,
		steps: append(q.steps, queryStep{
			direction: "both",
			predicate: "",
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
				// 获取出边和入边
				outNeighbors, _ := q.graph.GetNeighbors(ctx, node, step.predicate)
				inNeighbors, _ := q.graph.GetInNeighbors(ctx, node, step.predicate)

				// 处理出边
				for _, neighbor := range outNeighbors {
					triples = append(triples, cayley_driver.Triple{
						Subject:   node,
						Predicate: step.predicate,
						Object:    neighbor,
					})
					nextNodes = append(nextNodes, neighbor)
				}

				// 处理入边
				for _, neighbor := range inNeighbors {
					triples = append(triples, cayley_driver.Triple{
						Subject:   neighbor,
						Predicate: step.predicate,
						Object:    node,
					})
					nextNodes = append(nextNodes, neighbor)
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

