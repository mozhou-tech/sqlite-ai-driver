package imagesearch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// VectorSearch 向量搜索
type VectorSearch struct {
	db               *sql.DB
	tableName        string
	vectorColumn     string // 'text_embedding' 或 'image_embedding'
	embedder         Embedder
	docToEmbedding   func(map[string]any) ([]float64, error)
	embeddingLimiter *rate.Limiter
	limiterOnce      sync.Once
}

// addVectorSearch 添加向量搜索
// vectorColumn 应该是 'text_embedding' 或 'image_embedding'
func (r *ImageSearch) addVectorSearch(collection *Collection, vectorColumn string, docToEmbedding func(map[string]any) ([]float64, error)) (*VectorSearch, error) {
	// vectorColumn 已经是 'text_embedding' 或 'image_embedding'，不需要添加前缀
	// 列已经在 createCollection 中创建了

	return &VectorSearch{
		db:             r.db,
		tableName:      collection.tableName,
		vectorColumn:   vectorColumn,
		embedder:       nil, // 不再需要存储embedder，因为已经在docToEmbedding函数中使用了
		docToEmbedding: docToEmbedding,
	}, nil
}

// Search 执行向量搜索
func (v *VectorSearch) Search(ctx context.Context, embedding []float64, limit int, metadataFilter MetadataFilter) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	if len(embedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	}

	// 转换向量为字符串格式
	vectorStr := "["
	for i, val := range embedding {
		if i > 0 {
			vectorStr += ", "
		}
		vectorStr += fmt.Sprintf("%g", val)
	}
	vectorStr += "]"

	// 构建WHERE子句（根据对应的 embedding 字段是否有值来过滤）
	whereClause := fmt.Sprintf("%s IS NOT NULL AND embedding_status = 'completed'", v.vectorColumn)
	var queryArgs []any

	// 添加metadata过滤条件
	if metadataFilter != nil && len(metadataFilter) > 0 {
		// 使用SQLite的JSON函数来过滤metadata
		// 需要同时检查metadata字段和content字段中的metadata（因为metadata可能存储在content的JSON中）
		filterConditions := []string{}
		for key, value := range metadataFilter {
			// 转义key以防止SQL注入（虽然key来自map，但为了安全还是转义）
			// 使用json_extract_path_text来提取JSON字段值
			// 注意：SQLite的json_extract_path_text需要转义单引号
			escapedKey := strings.ReplaceAll(key, "'", "''")
			condition := fmt.Sprintf(
				"(json_extract_path_text(COALESCE(metadata, '{}'), '%s') = ? OR json_extract_path_text(content, '$.%s') = ?)",
				escapedKey, escapedKey,
			)
			filterConditions = append(filterConditions, condition)

			// 将value转换为字符串进行比较
			var valueStr string
			switch v := value.(type) {
			case string:
				valueStr = v
			case []byte:
				valueStr = string(v)
			default:
				// 对于其他类型，转换为JSON字符串进行比较
				valueBytes, _ := json.Marshal(value)
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

	// 使用SQLite的向量相似度搜索
	// 注意：参数顺序：第一个vectorStr用于SELECT，metadata过滤参数在中间，第二个vectorStr用于ORDER BY，最后是limit
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			list_cosine_similarity(%s, ?) as similarity
		FROM %s
		WHERE %s
		ORDER BY list_cosine_similarity(%s, ?) DESC
		LIMIT ?
	`, v.vectorColumn, v.tableName, whereClause, v.vectorColumn)

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
		var id int64
		var content string
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

		results = append(results, SearchResult{
			ID:      id,
			Content: getContentFromDoc(doc),
			Score:   similarity,
			Source:  "vector",
			Data:    doc,
		})
	}

	return results, nil
}

// processPendingEmbeddings 处理待处理的embedding
func (v *VectorSearch) processPendingEmbeddings(ctx context.Context) {
	// 获取embedding限制器
	v.limiterOnce.Do(func() {
		v.embeddingLimiter = rate.NewLimiter(rate.Limit(5), 1)
	})

	// 查询pending状态的文档（只查询对应 embedding 字段为空的文档）
	querySQL := fmt.Sprintf(`
		SELECT id, content, metadata
		FROM %s
		WHERE %s IS NULL AND embedding_status = 'pending'
		LIMIT 10
	`, v.tableName, v.vectorColumn)

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

		// 更新状态为processing
		updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'processing' WHERE id = ? AND %s IS NULL AND embedding_status = 'pending'`, v.tableName, v.vectorColumn)
		_, _ = v.db.ExecContext(ctx, updateStatusSQL, id)

		// 生成embedding
		if v.docToEmbedding != nil {
			// 等待速率限制
			_ = v.embeddingLimiter.Wait(ctx)

			embedding, err := v.docToEmbedding(doc)
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

				// 更新向量列和状态为completed
				updateVectorSQL := fmt.Sprintf(`UPDATE %s SET %s = ?, embedding_status = 'completed' WHERE id = ?`, v.tableName, v.vectorColumn)
				_, _ = v.db.ExecContext(ctx, updateVectorSQL, vectorStr, id)
			} else {
				// 更新状态为failed
				updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
				_, _ = v.db.ExecContext(ctx, updateStatusSQL, id)
			}
		}
	}
}
