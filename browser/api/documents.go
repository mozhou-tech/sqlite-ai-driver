package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// getDBInfo 获取数据库信息
func getDBInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name": "browser-db",
		"path": os.Getenv("DB_PATH"),
	})
}

// getCollections 获取所有集合
func getCollections(c *gin.Context) {
	query := `SELECT DISTINCT collection_name FROM documents`
	rows, err := sqlDB.Query(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	var collections []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		collections = append(collections, name)
	}

	collectionInfos := make([]CollectionInfo, len(collections))
	for i, name := range collections {
		collectionInfos[i] = CollectionInfo{
			Name:   name,
			Schema: make(map[string]interface{}),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"collections": collectionInfos,
	})
}

// getCollection 获取集合信息
func getCollection(c *gin.Context) {
	name := c.Param("name")

	var count int64
	query := `SELECT COUNT(*) FROM documents WHERE collection_name = ?`
	if err := sqlDB.QueryRow(query, name).Scan(&count); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":   name,
		"exists": count > 0,
		"count":  count,
	})
}

// getDocuments 获取集合中的所有文档
func getDocuments(c *gin.Context) {
	name := c.Param("name")
	limitStr := c.DefaultQuery("limit", "100")
	skipStr := c.DefaultQuery("skip", "0")
	tagFilter := c.Query("tag")

	limit, _ := strconv.Atoi(limitStr)
	skip, _ := strconv.Atoi(skipStr)

	logrus.WithFields(logrus.Fields{
		"collection": name,
		"limit":      limit,
		"skip":       skip,
		"tag":        tagFilter,
	}).Info("📄 getDocuments")

	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column, assuming it exists")
		hasEmbedding = true
	}

	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true
	}

	var baseQuery string
	if hasEmbedding && hasContent {
		baseQuery = `SELECT id, collection_name, data, embedding, content, created_at, updated_at FROM documents WHERE collection_name = ?`
	} else if hasEmbedding && !hasContent {
		baseQuery = `SELECT id, collection_name, data, embedding, NULL as content, created_at, updated_at FROM documents WHERE collection_name = ?`
	} else if !hasEmbedding && hasContent {
		baseQuery = `SELECT id, collection_name, data, NULL as embedding, content, created_at, updated_at FROM documents WHERE collection_name = ?`
	} else {
		baseQuery = `SELECT id, collection_name, data, NULL as embedding, NULL as content, created_at, updated_at FROM documents WHERE collection_name = ?`
	}
	args := []interface{}{name}

	if tagFilter != "" {
		baseQuery += ` AND json_extract(data, '$.tags') LIKE ?`
		args = append(args, "%"+tagFilter+"%")
	}

	countQuery := `SELECT COUNT(*) FROM documents WHERE collection_name = ?`
	countArgs := []interface{}{name}
	if tagFilter != "" {
		countQuery += ` AND json_extract(data, '$.tags') LIKE ?`
		countArgs = append(countArgs, "%"+tagFilter+"%")
	}

	var total int64
	if err := sqlDB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		logrus.WithError(err).Error("❌ Failed to count documents")
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	query := baseQuery + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, skip)

	rows, err := sqlDB.Query(query, args...)
	if err != nil {
		logrus.WithError(err).Error("❌ Failed to get documents")
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var doc Document
		var embeddingNull sql.NullString
		var contentNull sql.NullString
		if err := rows.Scan(&doc.ID, &doc.CollectionName, &doc.Data, &embeddingNull, &contentNull, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			logrus.WithError(err).Warn("Failed to scan document")
			continue
		}
		if embeddingNull.Valid {
			doc.Embedding = embeddingNull.String
		}
		if contentNull.Valid {
			doc.Content = contentNull.String
		}
		docs = append(docs, doc)
	}

	logrus.WithFields(logrus.Fields{
		"returned": len(docs),
		"total":    total,
		"skip":     skip,
		"limit":    limit,
	}).Info("📄 Returning documents")

	response := make([]DocumentResponse, len(docs))
	for i, doc := range docs {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(doc.Data), &data); err != nil {
			logrus.WithError(err).Warn("Failed to unmarshal document data")
			data = make(map[string]interface{})
		}
		response[i] = DocumentResponse{
			ID:   doc.ID,
			Data: data,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"documents": response,
		"total":     total,
		"skip":      skip,
		"limit":     limit,
	})
}

// getDocument 获取单个文档
func getDocument(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")

	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column, assuming it exists")
		hasEmbedding = true
	}

	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true
	}

	var doc Document
	var embeddingNull sql.NullString
	var contentNull sql.NullString
	var query string
	if hasEmbedding && hasContent {
		query = `SELECT id, collection_name, data, embedding, content, created_at, updated_at FROM documents WHERE collection_name = ? AND id = ?`
	} else if hasEmbedding && !hasContent {
		query = `SELECT id, collection_name, data, embedding, NULL as content, created_at, updated_at FROM documents WHERE collection_name = ? AND id = ?`
	} else if !hasEmbedding && hasContent {
		query = `SELECT id, collection_name, data, NULL as embedding, content, created_at, updated_at FROM documents WHERE collection_name = ? AND id = ?`
	} else {
		query = `SELECT id, collection_name, data, NULL as embedding, NULL as content, created_at, updated_at FROM documents WHERE collection_name = ? AND id = ?`
	}
	err = sqlDB.QueryRow(query, name, id).Scan(&doc.ID, &doc.CollectionName, &doc.Data, &embeddingNull, &contentNull, &doc.CreatedAt, &doc.UpdatedAt)
	if contentNull.Valid {
		doc.Content = contentNull.String
	}
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Document not found"})
		} else {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		}
		return
	}
	if embeddingNull.Valid {
		doc.Embedding = embeddingNull.String
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(doc.Data), &data); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, DocumentResponse{
		ID:   doc.ID,
		Data: data,
	})
}

// createDocument 创建文档
func createDocument(c *gin.Context) {
	name := c.Param("name")

	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	id, ok := data["id"].(string)
	if !ok || id == "" {
		id = generateID()
		data["id"] = id
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	content := extractTextFromData(string(dataJSON))
	contentTokens := tokenizeWithSego(content)

	var embeddingVector []float64
	if embeddingField, ok := data["embedding"]; ok {
		embeddingVector = extractEmbeddingVector(embeddingField)
	}

	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column, assuming it exists")
		hasEmbedding = true
	}

	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true
	}

	hasContentTokens, err := columnExists(sqlDB, "documents", "content_tokens")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content_tokens column, assuming it exists")
		hasContentTokens = true
	}

	columns := []string{"id", "collection_name", "data"}
	values := []interface{}{id, name, string(dataJSON)}
	placeholders := []string{"?", "?", "?"}

	if hasEmbedding && len(embeddingVector) > 0 {
		columns = append(columns, "embedding")
		values = append(values, embeddingVector)
		placeholders = append(placeholders, "?")
	}
	if hasContent {
		columns = append(columns, "content")
		values = append(values, content)
		placeholders = append(placeholders, "?")
	}
	if hasContentTokens {
		columns = append(columns, "content_tokens")
		values = append(values, contentTokens)
		placeholders = append(placeholders, "?")
	}

	columns = append(columns, "created_at", "updated_at")
	placeholders = append(placeholders, "CURRENT_TIMESTAMP", "CURRENT_TIMESTAMP")

	insertQuery := fmt.Sprintf("INSERT INTO documents (%s) VALUES (%s)",
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	_, err = sqlDB.Exec(insertQuery, values...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, DocumentResponse{
		ID:   id,
		Data: data,
	})
}

// updateDocument 更新文档
func updateDocument(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")

	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column, assuming it exists")
		hasEmbedding = true
	}

	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true
	}

	var doc Document
	var embeddingNull sql.NullString
	var contentNull sql.NullString
	var query string
	if hasEmbedding && hasContent {
		query = `SELECT id, collection_name, data, embedding, content FROM documents WHERE collection_name = ? AND id = ?`
	} else if hasEmbedding && !hasContent {
		query = `SELECT id, collection_name, data, embedding, NULL as content FROM documents WHERE collection_name = ? AND id = ?`
	} else if !hasEmbedding && hasContent {
		query = `SELECT id, collection_name, data, NULL as embedding, content FROM documents WHERE collection_name = ? AND id = ?`
	} else {
		query = `SELECT id, collection_name, data, NULL as embedding, NULL as content FROM documents WHERE collection_name = ? AND id = ?`
	}
	err = sqlDB.QueryRow(query, name, id).Scan(&doc.ID, &doc.CollectionName, &doc.Data, &embeddingNull, &contentNull)
	if contentNull.Valid {
		doc.Content = contentNull.String
	}
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Document not found"})
		} else {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		}
		return
	}
	if embeddingNull.Valid {
		doc.Embedding = embeddingNull.String
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(doc.Data), &data); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	for k, v := range updates {
		data[k] = v
	}

	data["id"] = id

	dataJSON, err := json.Marshal(data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	content := extractTextFromData(string(dataJSON))
	contentTokens := tokenizeWithSego(content)

	var embeddingVector []float64
	if embeddingField, ok := data["embedding"]; ok {
		embeddingVector = extractEmbeddingVector(embeddingField)
	}

	hasContentTokens, err := columnExists(sqlDB, "documents", "content_tokens")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content_tokens column, assuming it exists")
		hasContentTokens = true
	}

	setParts := []string{"data = ?"}
	values := []interface{}{string(dataJSON)}

	if hasEmbedding && len(embeddingVector) > 0 {
		setParts = append(setParts, "embedding = ?")
		values = append(values, embeddingVector)
	} else if hasEmbedding {
		setParts = append(setParts, "embedding = NULL")
	}
	if hasContent {
		setParts = append(setParts, "content = ?")
		values = append(values, content)
	}
	if hasContentTokens {
		setParts = append(setParts, "content_tokens = ?")
		values = append(values, contentTokens)
	}

	setParts = append(setParts, "updated_at = CURRENT_TIMESTAMP")
	values = append(values, name, id)

	updateQuery := fmt.Sprintf("UPDATE documents SET %s WHERE collection_name = ? AND id = ?",
		strings.Join(setParts, ", "))

	_, err = sqlDB.Exec(updateQuery, values...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, DocumentResponse{
		ID:   doc.ID,
		Data: data,
	})
}

// deleteDocument 删除文档
func deleteDocument(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")

	deleteQuery := `DELETE FROM documents WHERE collection_name = ? AND id = ?`
	_, err := sqlDB.Exec(deleteQuery, name, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Document deleted"})
}
