package textsearch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/snowflake"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"golang.org/x/time/rate"
)

// Embedder 向量嵌入生成器接口
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
}

// Options VecStore配置选项
type Options struct {
	Embedder Embedder
}

// VecStore 基于SQLite的纯文本向量搜索存储
type VecStore struct {
	db           *sql.DB
	tableName    string
	embedder     Embedder
	snowflake    *snowflake.Node
	initialized  bool
	mu           sync.Mutex
	limiter      *rate.Limiter
	limiterOnce  sync.Once
	isProcessing bool
}

// New 创建VecStore实例
func New(opts Options) *VecStore {
	// 初始化 Snowflake 生成器，使用节点 ID 1
	snowflakeNode, _ := snowflake.NewNode(1)
	return &VecStore{
		embedder:  opts.Embedder,
		tableName: "vecstore_documents",
		snowflake: snowflakeNode,
	}
}

// Initialize 初始化存储后端
// 使用 sqlite3-driver 的 SQLite 数据库
func (v *VecStore) Initialize(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.initialized {
		return nil
	}

	// 打开SQLite数据库连接
	// sqlite3-driver 会自动处理路径和 WAL 模式
	db, err := sql.Open("sqlite3", "vecstore.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	v.db = db

	// 创建表（如果不存在）
	// embedding 存储为 BLOB（JSON 格式的向量数组）
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INTEGER PRIMARY KEY NOT NULL,
			content TEXT,
			metadata TEXT,
			embedding BLOB,
			embedding_status TEXT DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			_rev INTEGER DEFAULT 1
		)
	`, v.tableName)

	_, err = v.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// 清理无效数据：删除 id 为 NULL 或空字符串的记录
	cleanupSQL := fmt.Sprintf(`
		DELETE FROM %s 
		WHERE id IS NULL OR id = ''
	`, v.tableName)
	if _, err := v.db.ExecContext(ctx, cleanupSQL); err != nil {
		// 记录错误但不阻止初始化
		log.Printf("[Initialize] 清理无效数据时出错: %v", err)
	} else {
		log.Printf("[Initialize] 已清理无效数据（id 为 NULL 或空字符串的记录）")
	}

	// 创建索引以提高查询性能
	createIndexSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_embedding_status_idx 
		ON %s (embedding_status)
	`, v.tableName, v.tableName)
	_, _ = v.db.ExecContext(ctx, createIndexSQL)

	v.initialized = true
	return nil
}

// Insert 插入文本
func (v *VecStore) Insert(ctx context.Context, text string, metadata map[string]any) error {
	if !v.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	if len([]rune(text)) == 0 {
		return fmt.Errorf("text cannot be empty")
	}

	// 使用 Snowflake 生成主键（int64 类型）
	id := v.snowflake.Generate()

	// 构建文档
	doc := map[string]any{
		"id":         id,
		"content":    text,
		"created_at": time.Now().Unix(),
	}

	// 合并元数据
	if metadata != nil {
		for k, v := range metadata {
			doc[k] = v
		}
	}

	// 将metadata字段序列化为JSON
	var metadataJSON string
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err == nil {
			metadataJSON = string(metadataBytes)
		}
	}
	// 如果metadataJSON为空，使用空JSON对象
	if metadataJSON == "" {
		metadataJSON = "{}"
	}

	// 将doc序列化为content字段（存储所有数据）
	contentJSON, _ := json.Marshal(doc)

	// 插入到数据库
	// 使用 Snowflake ID 作为主键，确保唯一性
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (id, content, metadata, _rev, embedding_status)
		VALUES (?, ?, ?, 1, 'pending')
	`, v.tableName)

	_, err := v.db.ExecContext(ctx, insertSQL, id, string(contentJSON), metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to insert text: %w", err)
	}

	// 如果提供了embedder，启动后台embedding处理
	if v.embedder != nil {
		v.processPendingEmbeddings(ctx)
	}

	return nil
}

// Search 执行向量搜索
func (v *VecStore) Search(ctx context.Context, query string, limit int, metadataFilter MetadataFilter) ([]SearchResult, error) {
	if !v.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if v.embedder == nil {
		return nil, fmt.Errorf("embedder not provided")
	}

	if limit <= 0 {
		limit = 10
	}

	// 生成查询向量
	queryEmbedding, err := v.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	}

	// 构建WHERE子句
	whereClause := "embedding IS NOT NULL AND embedding_status = 'completed'"
	var queryArgs []any

	// 添加metadata过滤条件
	if metadataFilter != nil && len(metadataFilter) > 0 {
		filterConditions := []string{}
		for key, value := range metadataFilter {
			// 转义key以防止SQL注入
			escapedKey := strings.ReplaceAll(key, "'", "''")
			// SQLite 使用 json_extract 函数
			condition := fmt.Sprintf(
				"(json_extract(COALESCE(metadata, '{}'), '$.%s') = ? OR json_extract(content, '$.%s') = ?)",
				escapedKey, escapedKey,
			)
			filterConditions = append(filterConditions, condition)

			// 将value转换为字符串进行比较
			var valueStr string
			switch val := value.(type) {
			case string:
				valueStr = val
			case []byte:
				valueStr = string(val)
			default:
				// 对于其他类型，转换为JSON字符串进行比较
				valueBytes, _ := json.Marshal(val)
				valueStr = string(valueBytes)
				// 移除JSON字符串的引号（如果是字符串值）
				if len(valueStr) >= 2 && valueStr[0] == '"' && valueStr[len(valueStr)-1] == '"' {
					valueStr = valueStr[1 : len(valueStr)-1]
				}
			}
			queryArgs = append(queryArgs, valueStr, valueStr)
		}
		if len(filterConditions) > 0 {
			whereClause += " AND (" + strings.Join(filterConditions, " AND ") + ")"
		}
	}

	// 查询所有候选文档（带metadata过滤）
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			embedding
		FROM %s
		WHERE %s
	`, v.tableName, whereClause)

	rows, err := v.db.QueryContext(ctx, sqlQuery, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to search vectors: %w", err)
	}
	defer rows.Close()

	// 在内存中计算余弦相似度并排序
	type candidateResult struct {
		id         int64
		content    string
		metadata   any
		doc        map[string]any
		similarity float64
	}

	var candidates []candidateResult
	for rows.Next() {
		var id int64
		var content string
		var metadataVal any
		var embeddingBlob []byte

		err := rows.Scan(&id, &content, &metadataVal, &embeddingBlob)
		if err != nil {
			continue
		}

		// 解析存储的向量（JSON格式）
		var storedEmbedding []float64
		if err := json.Unmarshal(embeddingBlob, &storedEmbedding); err != nil {
			continue
		}

		// 计算余弦相似度
		similarity := cosineSimilarity(queryEmbedding, storedEmbedding)

		var doc map[string]any
		if err := json.Unmarshal([]byte(content), &doc); err != nil {
			doc = map[string]any{"id": id, "content": content}
		}

		candidates = append(candidates, candidateResult{
			id:         id,
			content:    content,
			metadata:   metadataVal,
			doc:        doc,
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

	// 转换为 SearchResult
	results := make([]SearchResult, len(candidates))
	for i, cand := range candidates {
		// 提取文本内容
		textContent := ""
		if text, ok := cand.doc["content"].(string); ok {
			textContent = text
		}

		results[i] = SearchResult{
			ID:      cand.id,
			Content: textContent,
			Score:   cand.similarity,
			Data:    cand.doc,
		}
	}

	return results, nil
}

// Close 关闭VecStore
func (v *VecStore) Close() error {
	if v.db != nil {
		return v.db.Close()
	}
	return nil
}

// GetDB 返回内部的数据库连接（用于需要直接访问数据库的场景）
func (v *VecStore) GetDB() *sql.DB {
	return v.db
}

// GetTableName 返回表名
func (v *VecStore) GetTableName() string {
	return v.tableName
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

// SearchResult 搜索结果
type SearchResult struct {
	ID      int64
	Content string
	Score   float64
	Data    map[string]any
}

// MetadataFilter metadata过滤条件
// 支持按key-value进行过滤，多个条件之间为AND关系
type MetadataFilter map[string]any
