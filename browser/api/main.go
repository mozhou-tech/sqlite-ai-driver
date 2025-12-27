package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	gormDB    *gorm.DB
	sqlDB     *sql.DB
	graphDB   cayley_driver.Graph
	dbContext context.Context
)

// Document 文档模型
type Document struct {
	ID             string    `gorm:"primaryKey;type:varchar(255);not null"`
	CollectionName string    `gorm:"type:varchar(255);not null;index"`
	Data           string    `gorm:"type:text"` // JSON 格式存储
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

// TableName 指定表名
func (Document) TableName() string {
	return "documents"
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// CollectionInfo 集合信息
type CollectionInfo struct {
	Name   string                 `json:"name"`
	Schema map[string]interface{} `json:"schema"`
}

// DocumentResponse 文档响应
type DocumentResponse struct {
	ID   string                 `json:"id"`
	Data map[string]interface{} `json:"data"`
}

// FulltextSearchRequest 全文搜索请求
type FulltextSearchRequest struct {
	Collection string  `json:"collection"`
	Query      string  `json:"query"`
	Limit      int     `json:"limit"`
	Threshold  float64 `json:"threshold"`
}

// VectorSearchRequest 向量搜索请求
type VectorSearchRequest struct {
	Collection string    `json:"collection,omitempty"`
	Query      []float64 `json:"query,omitempty"`
	QueryText  string    `json:"query_text,omitempty"`
	Limit      int       `json:"limit,omitempty"`
	Field      string    `json:"field,omitempty"`
	Threshold  float64   `json:"threshold,omitempty"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error string `json:"error"`
}

func main() {
	// 从环境变量读取数据库配置
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "browser-db"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/browser-db"
	}

	// 确保数据目录存在
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		logrus.WithError(err).Fatal("Failed to create data directory")
	}

	ctx := context.Background()
	dbContext = ctx

	// 初始化 SQLite3 数据库（使用 GORM）
	// SQLite 需要文件路径，而不是目录路径
	sqliteDBPath := filepath.Join(dbPath, "browser.db")
	// 转换为绝对路径，避免工作目录问题
	absDBPath, err := filepath.Abs(sqliteDBPath)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get absolute path")
	}
	logrus.WithField("db_path", absDBPath).Info("Database path")
	// 使用 sqlite3-driver，支持自动路径处理
	gormDB, err = gorm.Open(sqlite.Open(absDBPath), &gorm.Config{})
	if err != nil {
		logrus.WithError(err).Fatal("Failed to connect database")
	}

	// 获取底层 sql.DB
	sqlDB, err = gormDB.DB()
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get database instance")
	}
	defer sqlDB.Close()

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 自动迁移
	if err := gormDB.AutoMigrate(&Document{}); err != nil {
		logrus.WithError(err).Fatal("Failed to migrate database")
	}

	// 创建全文搜索虚拟表（FTS5）
	if err := createFTS5Table(sqlDB); err != nil {
		logrus.WithError(err).Warn("Failed to create FTS5 table, fulltext search may not work")
	}

	// 初始化图数据库（使用 Cayley 驱动）
	graphDBPath := filepath.Join(dbPath, "graph.db")
	graphDB, err = cayley_driver.NewGraph(graphDBPath)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create graph database")
	}
	defer graphDB.Close()

	logrus.Info("Database initialized successfully")

	// 设置 Gin 路由
	r := gin.Default()

	// 配置 CORS
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	r.Use(cors.New(config))

	// API 路由
	api := r.Group("/api")
	{
		// 数据库信息
		api.GET("/db/info", getDBInfo)
		api.GET("/db/collections", getCollections)

		// 集合操作
		api.GET("/collections/:name", getCollection)
		api.GET("/collections/:name/documents", getDocuments)
		api.GET("/collections/:name/documents/:id", getDocument)
		api.POST("/collections/:name/documents", createDocument)
		api.PUT("/collections/:name/documents/:id", updateDocument)
		api.DELETE("/collections/:name/documents/:id", deleteDocument)

		// 全文搜索
		api.POST("/collections/:name/fulltext/search", fulltextSearch)

		// 向量搜索
		api.POST("/collections/:name/vector/search", vectorSearch)

		// 图数据库操作
		api.POST("/graph/link", graphLink)
		api.DELETE("/graph/link", graphUnlink)
		api.GET("/graph/neighbors/:nodeId", graphNeighbors)
		api.POST("/graph/path", graphPath)
		api.POST("/graph/query", graphQuery)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "40111"
	}

	logrus.WithField("port", port).Info("Server starting")
	if err := r.Run(":" + port); err != nil {
		logrus.WithError(err).Fatal("Failed to start server")
	}
}

// createFTS5Table 创建 FTS5 全文搜索虚拟表
func createFTS5Table(db *sql.DB) error {
	// 创建 FTS5 虚拟表用于全文搜索
	// 注意：FTS5 的 content_rowid 必须指向一个整数列，不能使用字符串 ID
	// 因此我们使用 rowid 作为关联，并在 id 字段中存储文档 ID
	createFTS5SQL := `
	CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
		id UNINDEXED,
		collection_name UNINDEXED,
		content
	);
	`
	_, err := db.Exec(createFTS5SQL)
	if err != nil {
		return err
	}

	// 创建触发器来自动同步 FTS5 索引
	// 当 documents 表插入时
	createTriggerSQL1 := `
	CREATE TRIGGER IF NOT EXISTS documents_fts_insert AFTER INSERT ON documents BEGIN
		INSERT INTO documents_fts(rowid, id, collection_name, content)
		VALUES (new.rowid, new.id, new.collection_name, ?);
	END;
	`
	// 注意：触发器中的 content 需要从 JSON 数据中提取，但触发器不支持函数调用
	// 所以我们手动维护索引

	// 当 documents 表更新时
	createTriggerSQL2 := `
	CREATE TRIGGER IF NOT EXISTS documents_fts_update AFTER UPDATE ON documents BEGIN
		UPDATE documents_fts SET
			id = new.id,
			collection_name = new.collection_name,
			content = ?
		WHERE rowid = new.rowid;
	END;
	`

	// 当 documents 表删除时
	createTriggerSQL3 := `
	CREATE TRIGGER IF NOT EXISTS documents_fts_delete AFTER DELETE ON documents BEGIN
		DELETE FROM documents_fts WHERE rowid = old.rowid;
	END;
	`

	// 由于触发器无法直接提取 JSON 内容，我们仍然需要手动维护索引
	// 但触发器可以帮助同步 rowid
	_, _ = db.Exec(createTriggerSQL1)
	_, _ = db.Exec(createTriggerSQL2)
	_, _ = db.Exec(createTriggerSQL3)

	return nil
}

// getDBInfo 获取数据库信息
func getDBInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name": "browser-db",
		"path": os.Getenv("DB_PATH"),
	})
}

// getCollections 获取所有集合
func getCollections(c *gin.Context) {
	var collections []string
	if err := gormDB.Model(&Document{}).
		Distinct("collection_name").
		Pluck("collection_name", &collections).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
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

	// 检查集合是否存在
	var count int64
	if err := gormDB.Model(&Document{}).
		Where("collection_name = ?", name).
		Count(&count).Error; err != nil {
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

	var docs []Document
	query := gormDB.Where("collection_name = ?", name)

	// 如果指定了 tag 过滤，需要在 JSON 数据中搜索
	if tagFilter != "" {
		// 使用 JSON 查询（SQLite 支持 JSON1 扩展）
		query = query.Where("json_extract(data, '$.tags') LIKE ?", "%"+tagFilter+"%")
	}

	// 获取总数
	var total int64
	if err := query.Model(&Document{}).Count(&total).Error; err != nil {
		logrus.WithError(err).Error("❌ Failed to count documents")
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// 分页查询
	if err := query.Offset(skip).Limit(limit).Find(&docs).Error; err != nil {
		logrus.WithError(err).Error("❌ Failed to get documents")
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
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

	var doc Document
	if err := gormDB.Where("collection_name = ? AND id = ?", name, id).First(&doc).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Document not found"})
		} else {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		}
		return
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

	// 生成 ID（如果未提供）
	id, ok := data["id"].(string)
	if !ok || id == "" {
		id = generateID()
		data["id"] = id
	}

	// 将数据序列化为 JSON
	dataJSON, err := json.Marshal(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	doc := Document{
		ID:             id,
		CollectionName: name,
		Data:           string(dataJSON),
	}

	if err := gormDB.Create(&doc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// 更新 FTS5 索引
	updateFTS5Index(sqlDB, doc)

	c.JSON(http.StatusCreated, DocumentResponse{
		ID:   doc.ID,
		Data: data,
	})
}

// updateDocument 更新文档
func updateDocument(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")

	var doc Document
	if err := gormDB.Where("collection_name = ? AND id = ?", name, id).First(&doc).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Document not found"})
		} else {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		}
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// 解析现有数据
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(doc.Data), &data); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// 合并更新
	for k, v := range updates {
		data[k] = v
	}

	// 确保 ID 不变
	data["id"] = id

	// 序列化回 JSON
	dataJSON, err := json.Marshal(data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	doc.Data = string(dataJSON)
	if err := gormDB.Save(&doc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// 更新 FTS5 索引
	updateFTS5Index(sqlDB, doc)

	c.JSON(http.StatusOK, DocumentResponse{
		ID:   doc.ID,
		Data: data,
	})
}

// deleteDocument 删除文档
func deleteDocument(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")

	if err := gormDB.Where("collection_name = ? AND id = ?", name, id).Delete(&Document{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// 从 FTS5 索引中删除
	deleteFTS5Index(sqlDB, id)

	c.JSON(http.StatusOK, gin.H{"message": "Document deleted"})
}

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

	// 使用 FTS5 进行全文搜索
	// 通过 rowid 关联 documents 表和 documents_fts 表
	query := `
	SELECT d.id, d.collection_name, d.data, 
	       bm25(documents_fts) as score
	FROM documents_fts
	JOIN documents d ON d.rowid = documents_fts.rowid
	WHERE d.collection_name = ? 
	  AND documents_fts MATCH ?
	ORDER BY score
	LIMIT ?
	`

	rows, err := sqlDB.Query(query, name, req.Query, req.Limit)
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

		// 检查阈值
		if req.Threshold > 0 && score > req.Threshold {
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

	// 如果提供了文本查询，生成 embedding
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

	start := time.Now()

	// 使用 SQLite 进行向量搜索
	// 从文档中提取 embedding 字段，计算余弦相似度
	// 注意：这需要文档的 data 字段中包含 embedding 数组

	// 首先获取集合中的所有文档
	var docs []Document
	if err := gormDB.Where("collection_name = ?", name).Find(&docs).Error; err != nil {
		logrus.WithError(err).Error("Failed to get documents for vector search")
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	type VectorResult struct {
		Document DocumentResponse
		Score    float64
	}

	var results []VectorResult

	// 遍历文档，计算相似度
	for _, doc := range docs {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(doc.Data), &data); err != nil {
			continue
		}

		// 获取文档的 embedding
		embeddingField, ok := data[req.Field]
		if !ok {
			continue
		}

		// 转换 embedding 为 []float64
		var docVector []float64
		switch v := embeddingField.(type) {
		case []interface{}:
			docVector = make([]float64, len(v))
			for i, val := range v {
				if f, ok := val.(float64); ok {
					docVector[i] = f
				} else if f, ok := val.(float32); ok {
					docVector[i] = float64(f)
				} else {
					// 跳过无效的 embedding
					docVector = nil
					break
				}
			}
		case []float64:
			docVector = v
		default:
			continue
		}

		if docVector == nil || len(docVector) == 0 {
			continue
		}

		// 计算余弦相似度
		similarity := cosineSimilarity(queryVector, docVector)

		// 应用阈值过滤（如果设置了）
		if req.Threshold > 0 && similarity < req.Threshold {
			continue
		}

		results = append(results, VectorResult{
			Document: DocumentResponse{
				ID:   doc.ID,
				Data: data,
			},
			Score: similarity,
		})
	}

	// 按相似度排序（降序）
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Score < results[j].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// 限制结果数量
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	took := time.Since(start).Milliseconds()

	// 转换为响应格式
	responseResults := make([]gin.H, len(results))
	for i, r := range results {
		responseResults[i] = gin.H{
			"document": r.Document,
			"score":    r.Score,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"results": responseResults,
		"query":   req.QueryText,
		"took":    took,
	})
}

// generateID 生成文档 ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// updateFTS5Index 更新 FTS5 索引
func updateFTS5Index(db *sql.DB, doc Document) {
	// 提取文本内容用于全文搜索
	content := extractTextFromData(doc.Data)

	// 首先获取文档的 rowid
	var rowid int64
	rowidQuery := `SELECT rowid FROM documents WHERE id = ? AND collection_name = ?`
	err := db.QueryRow(rowidQuery, doc.ID, doc.CollectionName).Scan(&rowid)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"doc_id":     doc.ID,
			"collection": doc.CollectionName,
		}).Warn("Failed to get document rowid for FTS5 index")
		return
	}

	// 使用 rowid 更新 FTS5 索引
	query := `
	INSERT INTO documents_fts(rowid, id, collection_name, content)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(rowid) DO UPDATE SET
		id = excluded.id,
		collection_name = excluded.collection_name,
		content = excluded.content
	`
	_, err = db.Exec(query, rowid, doc.ID, doc.CollectionName, content)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"doc_id": doc.ID,
			"rowid":  rowid,
		}).Warn("Failed to update FTS5 index")
	}
}

// deleteFTS5Index 从 FTS5 索引中删除
func deleteFTS5Index(db *sql.DB, id string) {
	// 首先获取文档的 rowid
	var rowid int64
	rowidQuery := `SELECT rowid FROM documents WHERE id = ?`
	err := db.QueryRow(rowidQuery, id).Scan(&rowid)
	if err != nil {
		logrus.WithError(err).WithField("doc_id", id).Warn("Failed to get document rowid for FTS5 deletion")
		return
	}

	// 使用 rowid 删除 FTS5 索引
	query := `DELETE FROM documents_fts WHERE rowid = ?`
	_, err = db.Exec(query, rowid)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"doc_id": id,
			"rowid":  rowid,
		}).Warn("Failed to delete from FTS5 index")
	}
}

// extractTextFromData 从 JSON 数据中提取文本内容
func extractTextFromData(dataJSON string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return ""
	}

	var parts []string
	for k, v := range data {
		if k == "id" || k == "_rev" {
			continue
		}
		if str, ok := v.(string); ok {
			parts = append(parts, str)
		} else {
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	return strings.Join(parts, " ")
}

// DashScope API 结构
type DashScopeEmbeddingRequest struct {
	Model string         `json:"model"`
	Input DashScopeInput `json:"input"`
}

type DashScopeInput struct {
	Texts []string `json:"texts"`
}

type DashScopeEmbeddingResponse struct {
	Output DashScopeOutput `json:"output"`
}

type DashScopeOutput struct {
	Embeddings []DashScopeEmbedding `json:"embeddings"`
}

type DashScopeEmbedding struct {
	Embedding []float32 `json:"embedding"`
}

// generateEmbeddingFromText 使用 DashScope API 从文本生成 embedding
func generateEmbeddingFromText(text string) ([]float64, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY environment variable is not set")
	}

	url := "https://dashscope.aliyuncs.com/api/v1/services/embeddings/text-embedding/text-embedding"

	reqBody := DashScopeEmbeddingRequest{
		Model: "text-embedding-v4",
		Input: DashScopeInput{
			Texts: []string{text},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp DashScopeEmbeddingResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResp.Output.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	embedding := apiResp.Output.Embeddings[0].Embedding
	result := make([]float64, len(embedding))
	for i, v := range embedding {
		result[i] = float64(v)
	}

	return result, nil
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

// sqrt 计算平方根（使用标准库）
func sqrt(x float64) float64 {
	return math.Sqrt(x)
}

// ========================================
// 图数据库 API 处理函数
// ========================================

type GraphLinkRequest struct {
	From     string `json:"from" binding:"required"`
	Relation string `json:"relation" binding:"required"`
	To       string `json:"to" binding:"required"`
}

type GraphPathRequest struct {
	From      string   `json:"from" binding:"required"`
	To        string   `json:"to" binding:"required"`
	MaxDepth  int      `json:"max_depth"`
	Relations []string `json:"relations,omitempty"`
}

type GraphQueryRequest struct {
	Query string `json:"query" binding:"required"`
}

// graphLink 创建图关系链接
func graphLink(c *gin.Context) {
	var req GraphLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	if err := graphDB.Link(dbContext, req.From, req.Relation, req.To); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Link created successfully",
		"from":     req.From,
		"relation": req.Relation,
		"to":       req.To,
	})
}

// graphUnlink 删除图关系链接
func graphUnlink(c *gin.Context) {
	var req GraphLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	if err := graphDB.Unlink(dbContext, req.From, req.Relation, req.To); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Link deleted successfully",
		"from":     req.From,
		"relation": req.Relation,
		"to":       req.To,
	})
}

// graphNeighbors 获取节点的邻居
func graphNeighbors(c *gin.Context) {
	nodeID := c.Param("nodeId")
	relation := c.DefaultQuery("relation", "")

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	neighbors, err := graphDB.GetNeighbors(dbContext, nodeID, relation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"node_id":   nodeID,
		"relation":  relation,
		"neighbors": neighbors,
	})
}

// graphPath 查找两个节点之间的路径
func graphPath(c *gin.Context) {
	var req GraphPathRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if req.MaxDepth == 0 {
		req.MaxDepth = 5
	}

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	var paths [][]string
	var err error

	// Cayley 驱动的 FindPath 只接受单个 predicate，这里简化处理
	predicate := ""
	if len(req.Relations) > 0 {
		predicate = req.Relations[0]
	}

	paths, err = graphDB.FindPath(dbContext, req.From, req.To, req.MaxDepth, predicate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"from":  req.From,
		"to":    req.To,
		"paths": paths,
	})
}

// graphQuery 执行图查询
func graphQuery(c *gin.Context) {
	var req GraphQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	query := graphDB.Query()
	if query == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Query builder not available",
		})
		return
	}

	logrus.WithField("query", req.Query).Info("🔍 解析查询字符串")

	// 解析 V('nodeId')
	if !strings.HasPrefix(req.Query, "V(") {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "查询必须以 V('nodeId') 开始",
		})
		return
	}

	// 提取节点ID
	var nodeID string
	var vEndIndex int

	nodeStart := strings.Index(req.Query, "('")
	if nodeStart == -1 {
		nodeStart = strings.Index(req.Query, "(\"")
		if nodeStart == -1 {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error: "无法解析节点ID，格式应为 V('nodeId') 或 V(\"nodeId\")",
			})
			return
		}
		relEnd := strings.Index(req.Query[nodeStart+2:], "\")")
		if relEnd == -1 {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "节点ID格式错误"})
			return
		}
		nodeID = req.Query[nodeStart+2 : nodeStart+2+relEnd]
		vEndIndex = nodeStart + 2 + relEnd + 2
	} else {
		relEnd := strings.Index(req.Query[nodeStart+2:], "')")
		if relEnd == -1 {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "节点ID格式错误"})
			return
		}
		nodeID = req.Query[nodeStart+2 : nodeStart+2+relEnd]
		vEndIndex = nodeStart + 2 + relEnd + 2
	}

	logrus.WithField("node_id", nodeID).Info("📌 提取节点ID")

	// 创建基础查询
	queryImpl := query.V(nodeID)
	if queryImpl == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "创建查询失败",
		})
		return
	}

	// 检查是否有 .Out() 或 .In()
	remainingQuery := ""
	if vEndIndex < len(req.Query) {
		remainingQuery = req.Query[vEndIndex:]
	}
	logrus.WithField("remaining_query", remainingQuery).Info("📋 剩余查询部分")

	if strings.HasPrefix(remainingQuery, ".Out(") {
		relStart := strings.Index(remainingQuery, "('")
		if relStart == -1 {
			relStart = strings.Index(remainingQuery, "(\"")
			if relStart == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无法解析关系名称"})
				return
			}
			relEnd := strings.Index(remainingQuery[relStart+2:], "\")")
			if relEnd == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "关系名称格式错误"})
				return
			}
			relation := remainingQuery[relStart+2 : relStart+2+relEnd]
			logrus.WithField("relation", relation).Info("🔗 提取关系 (Out)")
			queryImpl = queryImpl.Out(relation)
		} else {
			relEnd := strings.Index(remainingQuery[relStart+2:], "')")
			if relEnd == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "关系名称格式错误"})
				return
			}
			relation := remainingQuery[relStart+2 : relStart+2+relEnd]
			logrus.WithField("relation", relation).Info("🔗 提取关系 (Out)")
			queryImpl = queryImpl.Out(relation)
		}
	} else if strings.HasPrefix(remainingQuery, ".In(") {
		relStart := strings.Index(remainingQuery, "('")
		if relStart == -1 {
			relStart = strings.Index(remainingQuery, "(\"")
			if relStart == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无法解析关系名称"})
				return
			}
			relEnd := strings.Index(remainingQuery[relStart+2:], "\")")
			if relEnd == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "关系名称格式错误"})
				return
			}
			relation := remainingQuery[relStart+2 : relStart+2+relEnd]
			logrus.WithField("relation", relation).Info("🔗 提取关系 (In)")
			queryImpl = queryImpl.In(relation)
		} else {
			relEnd := strings.Index(remainingQuery[relStart+2:], "')")
			if relEnd == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "关系名称格式错误"})
				return
			}
			relation := remainingQuery[relStart+2 : relStart+2+relEnd]
			logrus.WithField("relation", relation).Info("🔗 提取关系 (In)")
			queryImpl = queryImpl.In(relation)
		}
	}

	if queryImpl == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "构建查询失败",
		})
		return
	}

	// 执行查询
	logrus.Info("🚀 执行图查询...")
	queryResults, err := queryImpl.All(dbContext)
	if err != nil {
		logrus.WithError(err).Info("❌ 查询执行失败")
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	logrus.WithField("count", len(queryResults)).Info("✅ 查询成功，找到结果")

	// 转换结果
	results := make([]gin.H, len(queryResults))
	for i, r := range queryResults {
		results[i] = gin.H{
			"subject":   r.Subject,
			"predicate": r.Predicate,
			"object":    r.Object,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"query":   req.Query,
		"results": results,
	})
}
