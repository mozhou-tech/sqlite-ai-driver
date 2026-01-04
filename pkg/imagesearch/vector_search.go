package imagesearch

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	sqlite3_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

// VectorSearch 向量搜索
type VectorSearch struct {
	db               *gorm.DB
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
			// 使用json_extract来提取JSON字段值
			// 注意：SQLite的json_extract使用 $ 路径语法
			escapedKey := strings.ReplaceAll(key, "'", "''")
			condition := fmt.Sprintf(
				"(json_extract(COALESCE(metadata, '{}'), '$.%s') = ? OR json_extract(content, '$.%s') = ?)",
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

	// 查询所有候选文档（在内存中计算相似度）
	// SQLite 不支持 list_cosine_similarity 函数，需要在内存中计算
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			%s as vector_embedding
		FROM %s
		WHERE %s
	`, v.vectorColumn, v.tableName, whereClause)

	// 构建参数列表：metadata过滤参数
	finalArgs := queryArgs

	// 使用 GORM 的 Raw SQL 查询
	type SearchRow struct {
		ID              []byte
		Content         string
		Metadata        string
		VectorEmbedding string
	}

	var rows []SearchRow
	if err := v.db.WithContext(ctx).Raw(sqlQuery, finalArgs...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to search vectors: %w", err)
	}

	// 在内存中计算余弦相似度并排序
	type candidateResult struct {
		id         string
		content    string
		doc        map[string]any
		similarity float64
	}

	var candidates []candidateResult
	for _, row := range rows {
		// 将二进制 UUID 转换为字符串
		id, err := sqlite3_driver.BytesToUUIDString(row.ID)
		if err != nil {
			continue // 跳过无效的 UUID
		}

		// 解析存储的向量（JSON格式字符串）
		var storedEmbedding []float64
		if row.VectorEmbedding != "" {
			if err := json.Unmarshal([]byte(row.VectorEmbedding), &storedEmbedding); err != nil {
				continue
			}
		}

		if len(storedEmbedding) == 0 || len(storedEmbedding) != len(embedding) {
			continue
		}

		// 计算余弦相似度
		similarity := cosineSimilarity(embedding, storedEmbedding)

		var doc map[string]any
		if err := json.Unmarshal([]byte(row.Content), &doc); err != nil {
			doc = map[string]any{"id": id, "content": row.Content}
		}

		candidates = append(candidates, candidateResult{
			id:         id,
			content:    row.Content,
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
		results[i] = SearchResult{
			ID:      cand.id,
			Content: getContentFromDoc(cand.doc),
			Score:   cand.similarity,
			Source:  "vector",
			Data:    cand.doc,
		}
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
	type PendingDoc struct {
		ID       []byte
		Content  string
		Metadata string
	}

	var pendingDocs []PendingDoc
	querySQL := fmt.Sprintf(`
		SELECT id, content, metadata
		FROM %s
		WHERE (%s IS NULL OR %s = '') AND (embedding_status = 'pending' OR embedding_status = 'processing')
		LIMIT 10
	`, v.tableName, v.vectorColumn, v.vectorColumn)

	if err := v.db.WithContext(ctx).Raw(querySQL).Scan(&pendingDocs).Error; err != nil {
		return
	}

	for _, pendingDoc := range pendingDocs {
		// 解析文档
		var doc map[string]any
		if err := json.Unmarshal([]byte(pendingDoc.Content), &doc); err != nil {
			continue
		}

		// 更新状态为processing（只更新pending状态的文档，避免并发问题）
		updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'processing' WHERE id = ? AND embedding_status = 'pending' AND (%s IS NULL OR %s = '')`, v.tableName, v.vectorColumn, v.vectorColumn)
		result := v.db.WithContext(ctx).Exec(updateStatusSQL, pendingDoc.ID)
		if result.Error != nil {
			continue
		}
		// 检查是否真的更新了行（可能已经被其他goroutine处理了）
		if result.RowsAffected == 0 {
			continue
		}

		// 生成embedding
		if v.docToEmbedding == nil {
			// 如果没有embedding函数，将状态设置为failed
			updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
			v.db.WithContext(ctx).Exec(updateStatusSQL, pendingDoc.ID)
			continue
		}

		// 等待速率限制
		if err := v.embeddingLimiter.Wait(ctx); err != nil {
			// 如果context被取消，更新状态回pending以便重试
			updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'pending' WHERE id = ?`, v.tableName)
			v.db.WithContext(ctx).Exec(updateStatusSQL, pendingDoc.ID)
			continue
		}

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

			// 更新向量列和状态为completed（只更新processing状态的文档）
			updateVectorSQL := fmt.Sprintf(`UPDATE %s SET %s = ?, embedding_status = 'completed' WHERE id = ? AND embedding_status = 'processing'`, v.tableName, v.vectorColumn)
			result = v.db.WithContext(ctx).Exec(updateVectorSQL, vectorStr, pendingDoc.ID)
			if result.Error != nil {
				// 如果更新失败，记录错误但继续处理
				continue
			}
			// 如果 RowsAffected == 0，说明状态已经被改变（可能被其他goroutine处理），这是正常的
		} else {
			// 更新状态为failed
			updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
			v.db.WithContext(ctx).Exec(updateStatusSQL, pendingDoc.ID)
		}
	}
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
