package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// fulltextSearch 全文搜索
func fulltextSearch(c *gin.Context) {
	name := c.Param("name")

	var req FulltextSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	start := time.Now()

	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true
	}

	if !hasContent {
		logrus.Warn("Content column does not exist, using data column for search")
		query := `
		SELECT id, collection_name, data, CAST(1.0 AS DOUBLE) as score
		FROM documents
		WHERE collection_name = ? 
		  AND data LIKE ?
		LIMIT ?
		`
		searchPattern := "%" + req.Query + "%"
		rows, err := sqlDB.Query(query, name, searchPattern, req.Limit)
		if err != nil {
			logrus.WithError(err).Error("Fulltext search failed")
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		defer rows.Close()

		var results []gin.H
		for rows.Next() {
			var docID, collectionName, dataJSON string
			var score float64
			if err := rows.Scan(&docID, &collectionName, &dataJSON, &score); err != nil {
				logrus.WithError(err).Error("Failed to scan row")
				continue
			}

			if req.Threshold > 0 && score < req.Threshold {
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
				logrus.WithError(err).Warn("Failed to unmarshal document data")
				continue
			}

			results = append(results, gin.H{
				"document": DocumentResponse{
					ID:   docID,
					Data: data,
				},
				"score": score,
			})
		}

		took := time.Since(start).Milliseconds()
		c.JSON(http.StatusOK, gin.H{
			"results": results,
			"query":   req.Query,
			"took":    took,
		})
		return
	}

	var indexExists bool
	checkSQL := `SELECT COUNT(*) FROM pragma_table_info('documents') WHERE name = 'content'`
	var count int
	if err := sqlDB.QueryRow(checkSQL).Scan(&count); err == nil && count > 0 {
		indexExists = false
	}

	if !indexExists {
		logrus.Warn("FTS index does not exist, attempting to create it")
		if err := createDuckDBFTSIndex(sqlDB); err != nil {
			logrus.WithError(err).Error("Failed to create FTS index")
		}
	}

	queryTokens := tokenizeWithSego(req.Query)

	hasContentTokens, err := columnExists(sqlDB, "documents", "content_tokens")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content_tokens column, assuming it exists")
		hasContentTokens = true
	}

	var query string
	var searchText string
	if hasContentTokens && queryTokens != "" {
		query = `
		SELECT id, collection_name, data, CAST(1.0 AS DOUBLE) as score
		FROM documents
		WHERE collection_name = ? 
		  AND content_tokens MATCH ?
		LIMIT ?
		`
		searchText = queryTokens
	} else {
		query = `
		SELECT id, collection_name, data, CAST(1.0 AS DOUBLE) as score
		FROM documents
		WHERE collection_name = ? 
		  AND content MATCH ?
		LIMIT ?
		`
		searchText = req.Query
	}

	rows, err := sqlDB.Query(query, name, searchText, req.Limit)
	if err != nil {
		logrus.WithError(err).Warn("FTS query failed, using LIKE query as fallback")
		if hasContentTokens && queryTokens != "" {
			query = `
			SELECT id, collection_name, data, CAST(1.0 AS DOUBLE) as score
			FROM documents
			WHERE collection_name = ? 
			  AND content_tokens LIKE ?
			LIMIT ?
			`
			searchPattern := "%" + queryTokens + "%"
			rows, err = sqlDB.Query(query, name, searchPattern, req.Limit)
		} else {
			query = `
			SELECT id, collection_name, data, CAST(1.0 AS DOUBLE) as score
			FROM documents
			WHERE collection_name = ? 
			  AND content LIKE ?
			LIMIT ?
			`
			searchPattern := "%" + req.Query + "%"
			rows, err = sqlDB.Query(query, name, searchPattern, req.Limit)
		}
		if err != nil {
			logrus.WithError(err).Error("Fulltext search failed")
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
	}
	defer rows.Close()

	var results []gin.H
	for rows.Next() {
		var docID, collectionName, dataJSON string
		var score float64
		if err := rows.Scan(&docID, &collectionName, &dataJSON, &score); err != nil {
			logrus.WithError(err).Error("Failed to scan row")
			continue
		}

		if req.Threshold > 0 && score < req.Threshold {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			logrus.WithError(err).Warn("Failed to unmarshal document data")
			continue
		}

		results = append(results, gin.H{
			"document": DocumentResponse{
				ID:   docID,
				Data: data,
			},
			"score": score,
		})
	}

	took := time.Since(start).Milliseconds()
	c.JSON(http.StatusOK, gin.H{
		"results": results,
		"query":   req.Query,
		"took":    took,
	})
}

// vectorSearch 向量搜索
func vectorSearch(c *gin.Context) {
	name := c.Param("name")

	bodyBytes, _ := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req VectorSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logrus.WithError(err).Error("Failed to bind JSON")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("Invalid request format: %v", err),
		})
		return
	}

	logrus.WithFields(logrus.Fields{
		"collection":   name,
		"hasQuery":     len(req.Query) > 0,
		"hasQueryText": req.QueryText != "",
		"queryText":    req.QueryText,
		"limit":        req.Limit,
		"field":        req.Field,
	}).Info("Vector search request")

	var queryVector []float64
	if req.QueryText != "" {
		logrus.WithField("queryText", req.QueryText).Info("🔄 Generating embedding from text")
		embedding, err := generateEmbeddingFromText(req.QueryText)
		if err != nil {
			logrus.WithError(err).Error("❌ Failed to generate embedding from text")
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error: fmt.Sprintf("Failed to generate embedding from text: %v", err),
			})
			return
		}
		queryVector = embedding
		logrus.WithFields(logrus.Fields{
			"dimension": len(queryVector),
			"first3":    queryVector[:min(3, len(queryVector))],
		}).Info("✅ Generated embedding")
	} else if len(req.Query) > 0 {
		queryVector = req.Query
		logrus.WithField("dimension", len(queryVector)).Info("Using provided vector")
	} else {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Either 'query' (vector) or 'query_text' (text) must be provided",
		})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	if req.Field == "" {
		req.Field = "embedding"
	}

	// 使用数据库向量搜索，失败则直接报错
	vectorSearchDB(c, name, req, queryVector)
}

// vectorSearchDB 使用数据库进行向量搜索
func vectorSearchDB(c *gin.Context, name string, req VectorSearchRequest, queryVector []float64) {
	start := time.Now()

	// 检查 embedding 列是否存在
	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Error("Failed to check embedding column")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to check embedding column: %v", err),
		})
		return
	}

	if !hasEmbedding {
		logrus.Error("embedding column does not exist")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "向量搜索功能不可用：embedding 列不存在。请确保已正确创建向量索引。",
		})
		return
	}

	// 将查询向量转换为 DuckDB 可以接受的格式
	// DuckDB 需要 FLOAT[] 类型
	vectorStr := "["
	for i, v := range queryVector {
		if i > 0 {
			vectorStr += ", "
		}
		vectorStr += fmt.Sprintf("%g", v)
	}
	vectorStr += "]"

	// 使用 DuckDB 的 list_cosine_similarity 进行向量搜索
	// list_cosine_similarity 返回距离（distance），距离越小相似度越高
	// 相似度 = 1 - 距离，所以按距离升序排列（相似度降序）
	query := `
		SELECT 
			id,
			collection_name,
			data,
			1 - list_cosine_similarity(embedding, ?::FLOAT[]) as similarity
		FROM documents
		WHERE collection_name = ? 
		  AND embedding IS NOT NULL
		ORDER BY list_cosine_similarity(embedding, ?::FLOAT[]) ASC
		LIMIT ?
	`

	rows, err := sqlDB.Query(query, vectorStr, name, vectorStr, req.Limit*2) // 获取更多结果以便过滤
	if err != nil {
		logrus.WithError(err).Error("Vector search query failed")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("向量搜索失败: %v", err),
		})
		return
	}
	defer rows.Close()

	var results []gin.H
	for rows.Next() {
		var docID, collectionName, dataJSON string
		var similarity float64
		if err := rows.Scan(&docID, &collectionName, &dataJSON, &similarity); err != nil {
			logrus.WithError(err).Error("Failed to scan row")
			continue
		}

		// 应用阈值过滤
		if req.Threshold > 0 && similarity < req.Threshold {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			logrus.WithError(err).Warn("Failed to unmarshal document data")
			continue
		}

		results = append(results, gin.H{
			"document": DocumentResponse{
				ID:   docID,
				Data: data,
			},
			"score": similarity,
		})

		// 达到限制数量后停止
		if len(results) >= req.Limit {
			break
		}
	}

	if err := rows.Err(); err != nil {
		logrus.WithError(err).Error("Error iterating rows")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("向量搜索处理失败: %v", err),
		})
		return
	}

	took := time.Since(start).Milliseconds()

	c.JSON(http.StatusOK, gin.H{
		"results": results,
		"query":   req.QueryText,
		"took":    took,
	})
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
