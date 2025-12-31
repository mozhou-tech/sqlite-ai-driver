package imagerag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
func (v *VectorSearch) Search(ctx context.Context, embedding []float64, limit int) ([]SearchResult, error) {
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

	// 使用DuckDB的list_cosine_similarity进行向量搜索
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			list_cosine_similarity(%s, ?::FLOAT[]) as similarity
		FROM %s
		WHERE %s IS NOT NULL AND embedding_status = 'completed'
		ORDER BY list_cosine_similarity(%s, ?::FLOAT[]) DESC
		LIMIT ?
	`, v.vectorColumn, v.tableName, v.vectorColumn, v.vectorColumn)

	rows, err := v.db.QueryContext(ctx, sqlQuery, vectorStr, vectorStr, limit)
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

