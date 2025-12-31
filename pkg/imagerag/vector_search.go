package imagerag

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
	vectorColumn     string
	embedder         Embedder
	docToEmbedding   func(map[string]any) ([]float64, error)
	embeddingLimiter *rate.Limiter
	limiterOnce      sync.Once
}

// addVectorSearch 添加向量搜索
func (r *ImageRAG) addVectorSearch(collection *Collection, identifier string, docToEmbedding func(map[string]any) ([]float64, error)) (*VectorSearch, error) {
	vectorColumn := "vector_" + identifier

	// 检查并创建vector列
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM information_schema.columns 
		WHERE table_name = '%s' AND column_name = ?
	`, collection.tableName)

	var count int
	err := r.db.QueryRowContext(context.Background(), checkColumnSQL, vectorColumn).Scan(&count)
	if err == nil && count == 0 {
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s FLOAT[]`, collection.tableName, vectorColumn)
		_, err = r.db.ExecContext(context.Background(), alterTableSQL)
		if err != nil {
			return nil, fmt.Errorf("failed to add vector column: %w", err)
		}
	}

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

	// 构建WHERE子句
	whereClause := fmt.Sprintf("%s IS NOT NULL AND embedding_status = 'completed'", v.vectorColumn)
	var queryArgs []any

	// 添加metadata过滤条件
	if metadataFilter != nil && len(metadataFilter) > 0 {
		// 使用DuckDB的JSON函数来过滤metadata
		// 需要同时检查metadata字段和content字段中的metadata（因为metadata可能存储在content的JSON中）
		filterConditions := []string{}
		for key, value := range metadataFilter {
			// 转义key以防止SQL注入（虽然key来自map，但为了安全还是转义）
			// 使用json_extract_path_text来提取JSON字段值
			// 注意：DuckDB的json_extract_path_text需要转义单引号
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

	// 使用DuckDB的list_cosine_similarity进行向量搜索
	// 注意：参数顺序：第一个vectorStr用于SELECT，metadata过滤参数在中间，第二个vectorStr用于ORDER BY，最后是limit
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			list_cosine_similarity(%s, ?::FLOAT[]) as similarity
		FROM %s
		WHERE %s
		ORDER BY list_cosine_similarity(%s, ?::FLOAT[]) DESC
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

	// 查询pending状态的文档
	querySQL := fmt.Sprintf(`
		SELECT id, content, metadata
		FROM %s
		WHERE embedding_status = 'pending'
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

		// 更新状态为processing
		updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'processing' WHERE id = ? AND embedding_status = 'pending'`, v.tableName)
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

				// 更新向量列
				updateVectorSQL := fmt.Sprintf(`UPDATE %s SET %s = ?::FLOAT[], embedding_status = 'completed' WHERE id = ?`, v.tableName, v.vectorColumn)
				_, _ = v.db.ExecContext(ctx, updateVectorSQL, vectorStr, id)
			} else {
				// 更新状态为failed
				updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
				_, _ = v.db.ExecContext(ctx, updateStatusSQL, id)
			}
		}
	}
}
