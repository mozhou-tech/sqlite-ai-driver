package graphstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

// Embedder 向量嵌入生成器接口
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
}

// Options GraphStore配置选项
type Options struct {
	Embedder   Embedder
	WorkingDir string // 工作目录，作为基础目录
	GraphDB    string // 图谱数据库路径（用于 cayley-driver）
	TableName  string // DuckDB 表名，默认为 "graphstore_entities"
}

// GraphStore 基于cayley-driver和duckdb-driver的纯图谱存储
// - 使用cayley-driver存储图谱关系（三元组）
// - 使用duckdb-driver的index.db共享数据库存储实体的embedding信息，用于向量检索
// - 支持语义检索图谱（通过向量相似度搜索找到相关实体，然后返回图谱关系）
type GraphStore struct {
	graph       cayley_driver.Graph
	db          *sql.DB
	embedder    Embedder
	tableName   string
	initialized bool
	mu          sync.Mutex
}

// New 创建GraphStore实例
func New(opts Options) (*GraphStore, error) {
	tableName := opts.TableName
	if tableName == "" {
		tableName = "graphstore_entities"
	}

	workingDir := opts.WorkingDir
	if workingDir == "" {
		return nil, fmt.Errorf("WorkingDir is required")
	}

	graphDB := opts.GraphDB
	if graphDB == "" {
		graphDB = "graphstore.db"
	}

	// 创建图谱数据库（使用 graphstore_ 表前缀）
	graph, err := cayley_driver.NewGraphWithNamespace(workingDir, graphDB, "graphstore_")
	if err != nil {
		return nil, fmt.Errorf("failed to create graph: %w", err)
	}

	return &GraphStore{
		graph:     graph,
		embedder:  opts.Embedder,
		tableName: tableName,
	}, nil
}

// Initialize 初始化存储后端
func (g *GraphStore) Initialize(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.initialized {
		return nil
	}

	// 打开DuckDB数据库用于向量检索
	// 注意：无论传入什么路径，都会被 duckdb-driver 统一映射到共享数据库文件 ./data/indexing/index.db
	// 向量检索使用 duckdb-driver 的 index.db 共享数据库，不同的业务模块通过表名区分
	db, err := sql.Open("duckdb", "graphstore.db")
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

// AddEntity 添加实体及其embedding信息
// entityID: 实体唯一标识符
// entityName: 实体名称（用于生成embedding）
// metadata: 实体的元数据
func (g *GraphStore) AddEntity(ctx context.Context, entityID, entityName string, metadata map[string]any) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	if entityID == "" {
		return fmt.Errorf("entityID cannot be empty")
	}

	if entityName == "" {
		entityName = entityID
	}

	// 将metadata序列化为JSON
	var metadataJSON string
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err == nil {
			metadataJSON = string(metadataBytes)
		}
	}

	// 如果提供了embedder，生成embedding
	var embeddingStr string
	embeddingStatus := "pending"
	if g.embedder != nil && entityName != "" {
		embedding, err := g.embedder.Embed(ctx, entityName)
		if err == nil && len(embedding) > 0 {
			// 转换为字符串格式
			vectorStr := "["
			for i, val := range embedding {
				if i > 0 {
					vectorStr += ", "
				}
				vectorStr += fmt.Sprintf("%g", val)
			}
			vectorStr += "]"
			embeddingStr = vectorStr
			embeddingStatus = "completed"
		}
	}

	// 插入或更新实体
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (entity_id, entity_name, metadata, embedding, embedding_status, updated_at)
		VALUES (?, ?, ?, %s, ?, CURRENT_TIMESTAMP)
		ON CONFLICT (entity_id) DO UPDATE SET
			entity_name = excluded.entity_name,
			metadata = excluded.metadata,
			embedding = excluded.embedding,
			embedding_status = excluded.embedding_status,
			updated_at = CURRENT_TIMESTAMP
	`, g.tableName, func() string {
		if embeddingStr != "" {
			return "?::FLOAT[]"
		}
		return "NULL"
	}())

	var err error
	if embeddingStr != "" {
		_, err = g.db.ExecContext(ctx, insertSQL, entityID, entityName, metadataJSON, embeddingStr, embeddingStatus)
	} else {
		_, err = g.db.ExecContext(ctx, insertSQL, entityID, entityName, metadataJSON, embeddingStatus)
	}

	if err != nil {
		return fmt.Errorf("failed to add entity: %w", err)
	}

	return nil
}

// Link 创建一条从 subject 到 object 的边，边的类型为 predicate
func (g *GraphStore) Link(ctx context.Context, subject, predicate, object string) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.Link(ctx, subject, predicate, object)
}

// Unlink 删除一条边
func (g *GraphStore) Unlink(ctx context.Context, subject, predicate, object string) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.Unlink(ctx, subject, predicate, object)
}

// GetNeighbors 获取指定节点的邻居节点
func (g *GraphStore) GetNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.GetNeighbors(ctx, node, predicate)
}

// GetInNeighbors 获取指向指定节点的邻居节点（入边）
func (g *GraphStore) GetInNeighbors(ctx context.Context, node, predicate string) ([]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.GetInNeighbors(ctx, node, predicate)
}

// SemanticSearchResult 语义搜索结果
type SemanticSearchResult struct {
	EntityID   string
	EntityName string
	Score      float64
	Metadata   map[string]any
	Triples    []Triple // 与该实体相关的三元组
}

// Triple 表示图数据库中的三元组（subject-predicate-object）
type Triple struct {
	Subject   string
	Predicate string
	Object    string
}

// SemanticSearch 语义检索图谱
// 通过查询文本找到相似的实体，然后返回这些实体在图谱中的关系
// query: 查询文本
// limit: 返回的实体数量限制
// maxDepth: 图谱遍历的最大深度（用于获取子图）
func (g *GraphStore) SemanticSearch(ctx context.Context, query string, limit int, maxDepth int) ([]SemanticSearchResult, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if g.embedder == nil {
		return nil, fmt.Errorf("embedder not provided")
	}

	if limit <= 0 {
		limit = 10
	}

	if maxDepth <= 0 {
		maxDepth = 2
	}

	// 生成查询向量
	queryEmbedding, err := g.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	}

	// 转换向量为字符串格式
	vectorStr := "["
	for i, val := range queryEmbedding {
		if i > 0 {
			vectorStr += ", "
		}
		vectorStr += fmt.Sprintf("%g", val)
	}
	vectorStr += "]"

	// 使用DuckDB的list_cosine_similarity进行向量搜索
	// 向量检索使用 duckdb-driver 的 index.db 共享数据库
	sqlQuery := fmt.Sprintf(`
		SELECT 
			entity_id,
			entity_name,
			metadata,
			list_cosine_similarity(embedding, ?::FLOAT[]) as similarity
		FROM %s
		WHERE embedding IS NOT NULL AND embedding_status = 'completed'
		ORDER BY list_cosine_similarity(embedding, ?::FLOAT[]) DESC
		LIMIT ?
	`, g.tableName)

	rows, err := g.db.QueryContext(ctx, sqlQuery, vectorStr, vectorStr, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search entities: %w", err)
	}
	defer rows.Close()

	var results []SemanticSearchResult
	entityIDs := make(map[string]bool)

	for rows.Next() {
		var entityID, entityName string
		var metadataVal any
		var similarity float64

		err := rows.Scan(&entityID, &entityName, &metadataVal, &similarity)
		if err != nil {
			continue
		}

		// 解析metadata
		var metadata map[string]any
		if metadataVal != nil {
			if metadataStr, ok := metadataVal.(string); ok {
				_ = json.Unmarshal([]byte(metadataStr), &metadata)
			}
		}
		if metadata == nil {
			metadata = make(map[string]any)
		}

		// 获取该实体在图谱中的关系（子图）
		triples := g.getSubgraphTriples(ctx, entityID, maxDepth)

		results = append(results, SemanticSearchResult{
			EntityID:   entityID,
			EntityName: entityName,
			Score:      similarity,
			Metadata:   metadata,
			Triples:    triples,
		})

		entityIDs[entityID] = true
	}

	return results, nil
}

// getSubgraphTriples 获取实体的子图三元组
func (g *GraphStore) getSubgraphTriples(ctx context.Context, entityID string, maxDepth int) []Triple {
	var triples []Triple
	visited := make(map[string]bool)
	tripleSet := make(map[string]bool) // 用于去重：key = "subject|predicate|object"
	queue := []struct {
		node  string
		depth int
	}{{entityID, 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth || visited[current.node] {
			continue
		}
		visited[current.node] = true

		// 使用 Query API 的 Both() 方法获取所有相关的三元组
		query := g.graph.Query()
		allTriples, err := query.V(current.node).Both().All(ctx)
		if err != nil {
			continue
		}

		// 处理获取到的三元组
		for _, t := range allTriples {
			// 创建唯一键用于去重
			key := fmt.Sprintf("%s|%s|%s", t.Subject, t.Predicate, t.Object)
			if !tripleSet[key] {
				tripleSet[key] = true
				triples = append(triples, Triple{
					Subject:   t.Subject,
					Predicate: t.Predicate,
					Object:    t.Object,
				})

				// 确定下一个要遍历的节点
				var nextNode string
				if t.Subject == current.node {
					nextNode = t.Object
				} else {
					nextNode = t.Subject
				}

				// 如果还没访问过且深度未超限，加入队列
				if !visited[nextNode] && current.depth < maxDepth-1 {
					queue = append(queue, struct {
						node  string
						depth int
					}{nextNode, current.depth + 1})
				}
			}
		}
	}

	return triples
}

// GetSubgraph 获取子图
// entityID: 实体ID
// maxDepth: 最大深度
func (g *GraphStore) GetSubgraph(ctx context.Context, entityID string, maxDepth int) ([]Triple, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.getSubgraphTriples(ctx, entityID, maxDepth), nil
}

// Query 返回查询构建器，支持类似 Gremlin 的查询语法
func (g *GraphStore) Query() cayley_driver.GraphQuery {
	if !g.initialized {
		return nil
	}
	return g.graph.Query()
}

// FindPath 查找从 from 到 to 的路径
func (g *GraphStore) FindPath(ctx context.Context, from, to string, maxDepth int, predicate string) ([][]string, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	return g.graph.FindPath(ctx, from, to, maxDepth, predicate)
}

// AllTriples 获取图中所有的三元组
func (g *GraphStore) AllTriples(ctx context.Context) ([]Triple, error) {
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

// GetEntity 获取实体信息
func (g *GraphStore) GetEntity(ctx context.Context, entityID string) (map[string]any, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	querySQL := fmt.Sprintf(`
		SELECT entity_id, entity_name, metadata, embedding_status
		FROM %s
		WHERE entity_id = ?
	`, g.tableName)

	row := g.db.QueryRowContext(ctx, querySQL, entityID)

	var id, name string
	var metadataVal any
	var embeddingStatus string

	err := row.Scan(&id, &name, &metadataVal, &embeddingStatus)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entity not found: %s", entityID)
		}
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	result := map[string]any{
		"entity_id":        id,
		"entity_name":      name,
		"embedding_status": embeddingStatus,
	}

	// 解析metadata
	if metadataVal != nil {
		if metadataStr, ok := metadataVal.(string); ok {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil {
				result["metadata"] = metadata
			}
		}
	}

	return result, nil
}

// ListEntities 列出所有实体
func (g *GraphStore) ListEntities(ctx context.Context, limit int, offset int) ([]map[string]any, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if limit <= 0 {
		limit = 100
	}

	querySQL := fmt.Sprintf(`
		SELECT entity_id, entity_name, metadata, embedding_status, created_at
		FROM %s
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, g.tableName)

	rows, err := g.db.QueryContext(ctx, querySQL, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, name string
		var metadataVal any
		var embeddingStatus string
		var createdAt time.Time

		err := rows.Scan(&id, &name, &metadataVal, &embeddingStatus, &createdAt)
		if err != nil {
			continue
		}

		result := map[string]any{
			"entity_id":        id,
			"entity_name":      name,
			"embedding_status": embeddingStatus,
			"created_at":       createdAt,
		}

		// 解析metadata
		if metadataVal != nil {
			if metadataStr, ok := metadataVal.(string); ok {
				var metadata map[string]any
				if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil {
					result["metadata"] = metadata
				}
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// Close 关闭GraphStore
func (g *GraphStore) Close() error {
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
		return fmt.Errorf("errors closing graphstore: %v", errs)
	}

	return nil
}
