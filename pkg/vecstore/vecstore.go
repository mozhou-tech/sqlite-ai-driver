package vecstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
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

// VecStore 基于DuckDB的纯文本向量搜索存储
type VecStore struct {
	db          *sql.DB
	tableName   string
	embedder    Embedder
	initialized bool
	mu          sync.Mutex
	limiter     *rate.Limiter
	limiterOnce sync.Once
}

// New 创建VecStore实例
func New(opts Options) *VecStore {
	return &VecStore{
		embedder:  opts.Embedder,
		tableName: "vecstore_documents",
	}
}

// Initialize 初始化存储后端
// 使用 duckdb-driver 的共享数据库 ./data/indexing/index.db
func (v *VecStore) Initialize(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.initialized {
		return nil
	}

	// 打开DuckDB数据库连接
	// duckdb-driver 会自动将所有路径映射到共享数据库文件 ./data/indexing/index.db
	db, err := sql.Open("duckdb", "vecstore.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	v.db = db

	// 创建表（如果不存在）
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR PRIMARY KEY,
			content TEXT,
			metadata JSON,
			embedding FLOAT[],
			embedding_status VARCHAR DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			_rev INTEGER DEFAULT 1
		)
	`, v.tableName)

	_, err = v.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

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

	// 生成ID
	id := fmt.Sprintf("%d", time.Now().UnixNano())

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

	// 转换向量为字符串格式
	vectorStr := "["
	for i, val := range queryEmbedding {
		if i > 0 {
			vectorStr += ", "
		}
		vectorStr += fmt.Sprintf("%g", val)
	}
	vectorStr += "]"

	// 构建WHERE子句
	whereClause := "embedding IS NOT NULL AND embedding_status = 'completed'"
	var queryArgs []any

	// 添加metadata过滤条件
	if metadataFilter != nil && len(metadataFilter) > 0 {
		filterConditions := []string{}
		for key, value := range metadataFilter {
			// 转义key以防止SQL注入
			escapedKey := strings.ReplaceAll(key, "'", "''")
			condition := fmt.Sprintf(
				"(json_extract_path_text(COALESCE(metadata, '{}'), '%s') = ? OR json_extract_path_text(content, '$.%s') = ?)",
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

	// 使用DuckDB的list_cosine_similarity进行向量搜索
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			list_cosine_similarity(embedding, ?::FLOAT[]) as similarity
		FROM %s
		WHERE %s
		ORDER BY list_cosine_similarity(embedding, ?::FLOAT[]) DESC
		LIMIT ?
	`, v.tableName, whereClause)

	// 构建完整的参数列表：vectorStr（SELECT用）+ metadata过滤参数 + vectorStr（ORDER BY用）+ limit
	finalArgs := []any{vectorStr}
	finalArgs = append(finalArgs, queryArgs...)
	finalArgs = append(finalArgs, vectorStr, limit)

	rows, err := v.db.QueryContext(ctx, sqlQuery, finalArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to search vectors: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var id, content string
		var metadataVal any
		var similarity float64

		err := rows.Scan(&id, &content, &metadataVal, &similarity)
		if err != nil {
			continue
		}

		var doc map[string]any
		if err := json.Unmarshal([]byte(content), &doc); err != nil {
			doc = map[string]any{"id": id, "content": content}
		}

		// 提取文本内容
		textContent := ""
		if text, ok := doc["content"].(string); ok {
			textContent = text
		}

		results = append(results, SearchResult{
			ID:      id,
			Content: textContent,
			Score:   similarity,
			Data:    doc,
		})
	}

	return results, nil
}

// processPendingEmbeddings 处理待处理的embedding
func (v *VecStore) processPendingEmbeddings(ctx context.Context) {
	// 获取embedding限制器
	v.limiterOnce.Do(func() {
		v.limiter = rate.NewLimiter(rate.Limit(5), 1)
	})

	// 查询pending状态的文档
	querySQL := fmt.Sprintf(`
		SELECT id, content, metadata
		FROM %s
		WHERE embedding IS NULL AND embedding_status = 'pending'
		LIMIT 10
	`, v.tableName)

	rows, err := v.db.QueryContext(ctx, querySQL)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, content string
		var metadataVal any

		if err := rows.Scan(&id, &content, &metadataVal); err != nil {
			continue
		}

		// 解析文档
		var doc map[string]any
		if err := json.Unmarshal([]byte(content), &doc); err != nil {
			continue
		}

		// 提取文本内容
		textContent := ""
		if text, ok := doc["content"].(string); ok {
			textContent = text
		}
		if textContent == "" {
			continue
		}

		// 更新状态为processing
		updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'processing' WHERE id = ? AND embedding IS NULL AND embedding_status = 'pending'`, v.tableName)
		_, _ = v.db.ExecContext(ctx, updateStatusSQL, id)

		// 生成embedding
		if v.embedder != nil {
			// 等待速率限制
			_ = v.limiter.Wait(ctx)

			embedding, err := v.embedder.Embed(ctx, textContent)
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

				// 更新向量列和状态
				updateVectorSQL := fmt.Sprintf(`UPDATE %s SET embedding = ?::FLOAT[], embedding_status = 'completed' WHERE id = ?`, v.tableName)
				_, _ = v.db.ExecContext(ctx, updateVectorSQL, vectorStr, id)
			} else {
				// 更新状态为failed
				updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
				_, _ = v.db.ExecContext(ctx, updateStatusSQL, id)
			}
		}
	}
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

// SearchResult 搜索结果
type SearchResult struct {
	ID      string
	Content string
	Score   float64
	Data    map[string]any
}

// MetadataFilter metadata过滤条件
// 支持按key-value进行过滤，多个条件之间为AND关系
type MetadataFilter map[string]any
