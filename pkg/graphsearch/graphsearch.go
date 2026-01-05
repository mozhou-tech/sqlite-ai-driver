package graphsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Embedder 向量嵌入生成器接口
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
}

// Entity GORM 模型，表示图谱存储中的实体
type Entity struct {
	EntityID        string    `gorm:"type:VARCHAR;primaryKey;not null"`
	EntityName      string    `gorm:"type:TEXT"`
	Metadata        string    `gorm:"type:TEXT"` // JSON 格式的元数据
	Embedding       []byte    `gorm:"type:BLOB"` // JSON 格式的向量数组
	EmbeddingStatus string    `gorm:"type:VARCHAR;default:'pending'"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime"`
}

// TableName 指定表名
func (Entity) TableName() string {
	return "graphstore_entities"
}

// Options GraphStore配置选项
type Options struct {
	Embedder   Embedder
	WorkingDir string // 工作目录，作为基础目录
	TableName  string // SQLite 表名，默认为 "graphstore_entities"
}

// GraphStore 基于cayley-driver和sqlite3-driver的纯图谱存储
// - 使用cayley-driver存储图谱关系（三元组）
// - 使用sqlite3-driver和GORM存储实体的embedding信息，用于向量检索
// - 支持语义检索图谱（通过向量相似度搜索找到相关实体，然后返回图谱关系）
type GraphStore struct {
	graph       cayley_driver.Graph
	db          *gorm.DB
	embedder    Embedder
	tableName   string
	workingDir  string
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

	// 创建图谱数据库（使用 graphstore_ 表前缀）
	graph, err := cayley_driver.NewGraphWithNamespace(workingDir, cayley_driver.GRAPH_DB_FILE, "graphstore_")
	if err != nil {
		return nil, fmt.Errorf("failed to create graph: %w", err)
	}

	return &GraphStore{
		graph:      graph,
		embedder:   opts.Embedder,
		tableName:  tableName,
		workingDir: workingDir,
	}, nil
}

// Initialize 初始化存储后端
// 使用 sqlite3-driver 的 SQLite 数据库和 GORM
func (g *GraphStore) Initialize(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.initialized {
		return nil
	}

	// 打开SQLite数据库连接，使用 GORM
	// sqlite3-driver 会自动处理路径和 WAL 模式
	// 使用 workingDir 参数来指定工作目录
	dsn := fmt.Sprintf("graphstore.db?workingDir=%s", g.workingDir)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	g.db = db

	// 使用 GORM AutoMigrate 创建表（如果不存在）
	// 使用 Table() 方法指定表名
	entityModel := Entity{}
	if err := g.db.WithContext(ctx).Table(g.tableName).AutoMigrate(&entityModel); err != nil {
		return fmt.Errorf("failed to migrate table: %w", err)
	}

	// 创建索引以提高查询性能
	if err := g.db.Exec(fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_entity_name 
		ON %s (entity_name)
	`, g.tableName, g.tableName)).Error; err != nil {
		return fmt.Errorf("failed to create entity_name index: %w", err)
	}

	if err := g.db.Exec(fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_embedding_status 
		ON %s (embedding_status)
	`, g.tableName, g.tableName)).Error; err != nil {
		return fmt.Errorf("failed to create embedding_status index: %w", err)
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
	if metadataJSON == "" {
		metadataJSON = "{}"
	}

	// 如果提供了embedder，生成embedding
	var embeddingBytes []byte
	embeddingStatus := "pending"
	if g.embedder != nil && entityName != "" {
		embedding, err := g.embedder.Embed(ctx, entityName)
		if err == nil && len(embedding) > 0 {
			// 将向量序列化为JSON格式的字节数组
			embeddingBytes, err = json.Marshal(embedding)
			if err == nil {
				embeddingStatus = "completed"
			}
		}
	}

	// 创建实体模型
	entity := Entity{
		EntityID:        entityID,
		EntityName:      entityName,
		Metadata:        metadataJSON,
		Embedding:       embeddingBytes,
		EmbeddingStatus: embeddingStatus,
	}

	// 使用 GORM 的 Save 方法（如果主键存在则更新，否则插入）
	// 使用 Table() 方法指定表名
	if err := g.db.WithContext(ctx).Table(g.tableName).Save(&entity).Error; err != nil {
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

	// 使用 GORM 查询所有候选实体
	var entities []Entity
	if err := g.db.WithContext(ctx).Table(g.tableName).
		Where("embedding IS NOT NULL AND embedding_status = ?", "completed").
		Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("failed to search entities: %w", err)
	}

	// 在内存中计算余弦相似度并排序
	type candidateResult struct {
		entity     Entity
		similarity float64
	}

	var candidates []candidateResult
	for _, entity := range entities {
		// 解析存储的向量（JSON格式）
		var storedEmbedding []float64
		if err := json.Unmarshal(entity.Embedding, &storedEmbedding); err != nil {
			continue
		}

		// 计算余弦相似度
		similarity := cosineSimilarity(queryEmbedding, storedEmbedding)

		candidates = append(candidates, candidateResult{
			entity:     entity,
			similarity: similarity,
		})
	}

	// 按相似度降序排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].similarity > candidates[j].similarity
	})

	// 取前 limit 个结果
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	// 转换为 SemanticSearchResult
	var results []SemanticSearchResult
	entityIDs := make(map[string]bool)

	for _, cand := range candidates {
		entity := cand.entity

		// 解析metadata
		var metadata map[string]any
		if entity.Metadata != "" {
			_ = json.Unmarshal([]byte(entity.Metadata), &metadata)
		}
		if metadata == nil {
			metadata = make(map[string]any)
		}

		// 获取该实体在图谱中的关系（子图）
		triples := g.getSubgraphTriples(ctx, entity.EntityID, maxDepth)

		results = append(results, SemanticSearchResult{
			EntityID:   entity.EntityID,
			EntityName: entity.EntityName,
			Score:      cand.similarity,
			Metadata:   metadata,
			Triples:    triples,
		})

		entityIDs[entity.EntityID] = true
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

	var entity Entity
	if err := g.db.WithContext(ctx).Table(g.tableName).
		Where("entity_id = ?", entityID).
		First(&entity).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("entity not found: %s", entityID)
		}
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	result := map[string]any{
		"entity_id":        entity.EntityID,
		"entity_name":      entity.EntityName,
		"embedding_status": entity.EmbeddingStatus,
	}

	// 解析metadata
	if entity.Metadata != "" {
		var metadata map[string]any
		if err := json.Unmarshal([]byte(entity.Metadata), &metadata); err == nil {
			result["metadata"] = metadata
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

	var entities []Entity
	if err := g.db.WithContext(ctx).Table(g.tableName).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}

	results := make([]map[string]any, len(entities))
	for i, entity := range entities {
		result := map[string]any{
			"entity_id":        entity.EntityID,
			"entity_name":      entity.EntityName,
			"embedding_status": entity.EmbeddingStatus,
			"created_at":       entity.CreatedAt,
		}

		// 解析metadata
		if entity.Metadata != "" {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(entity.Metadata), &metadata); err == nil {
				result["metadata"] = metadata
			}
		}

		results[i] = result
	}

	return results, nil
}

// cosineSimilarity 计算两个向量的余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Close 关闭GraphStore
func (g *GraphStore) Close() error {
	var errs []error

	if g.graph != nil {
		if err := g.graph.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// GORM 的 DB 不需要显式关闭，但我们可以获取底层 sql.DB 来关闭
	if g.db != nil {
		sqlDB, err := g.db.DB()
		if err == nil && sqlDB != nil {
			if err := sqlDB.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing graphstore: %v", errs)
	}

	return nil
}
