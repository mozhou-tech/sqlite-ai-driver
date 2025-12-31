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
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"github.com/sirupsen/logrus"
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
	Data           string    `gorm:"type:text"`     // JSON 格式存储
	Embedding      string    `gorm:"type:DOUBLE[]"` // 向量数据，存储为数组
	Content        string    `gorm:"type:text"`     // 提取的文本内容，用于全文搜索
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
	// 预加载 sego 词典
	if err := sego.Init(); err != nil {
		logrus.WithError(err).Warn("Failed to initialize sego dictionary")
	}

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

	// 初始化 DuckDB 数据库
	// DuckDB 需要文件路径，而不是目录路径
	duckDBPath := filepath.Join(dbPath, "browser.db")
	// 转换为绝对路径，避免工作目录问题
	absDBPath, err := filepath.Abs(duckDBPath)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get absolute path")
	}
	logrus.WithField("db_path", absDBPath).Info("Database path")

	// 直接使用 database/sql 连接 DuckDB
	sqlDB, err = sql.Open("duckdb", absDBPath)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to connect database")
	}
	defer sqlDB.Close()

	// 由于 GORM 不支持直接使用 sql.DB 连接 DuckDB
	// 我们使用原生 SQL 来创建表和执行操作
	// GORM 变量保留用于兼容性，但实际使用 sqlDB 进行操作

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 确保扩展已加载（duckdb-driver 会自动加载，但我们可以验证）
	if err := ensureDuckDBExtensions(sqlDB); err != nil {
		logrus.WithError(err).Warn("Some DuckDB extensions may not be available")
	}

	// 使用原生 SQL 创建表（DuckDB 兼容 SQLite 语法）
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS documents (
		id VARCHAR(255) PRIMARY KEY,
		collection_name VARCHAR(255) NOT NULL,
		data TEXT,
		embedding TEXT,
		content TEXT,
		content_tokens TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_documents_collection ON documents(collection_name);
	`
	if _, err := sqlDB.Exec(createTableSQL); err != nil {
		logrus.WithError(err).Fatal("Failed to create documents table")
	}

	// 创建全文搜索索引（使用 DuckDB FTS 扩展）
	if err := createDuckDBFTSIndex(sqlDB); err != nil {
		logrus.WithError(err).Error("Failed to create FTS index, fulltext search may not work")
		// 不退出程序，但记录错误，后续会在搜索时检查索引是否存在
	} else {
		logrus.Info("DuckDB FTS index created successfully")
	}

	// 创建向量索引（使用 DuckDB VSS 扩展）
	if err := createDuckDBVectorIndex(sqlDB); err != nil {
		logrus.WithError(err).Error("Failed to create vector index, vector search may not work")
		// 不退出程序，但记录错误，后续会在搜索时检查索引是否存在
	} else {
		logrus.Info("DuckDB vector index created successfully")
	}

	// 初始化图数据库（使用 Cayley 驱动）
	// 使用 dbPath 作为 workingDir，相对路径会构建到 {dbPath}/graph/ 目录
	graphDBPath := "graph.db"
	graphDB, err = cayley_driver.NewGraphWithNamespace(dbPath, graphDBPath, "")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create graph database")
	}
	defer graphDB.Close()

	// 注意：由于 GORM 不支持直接使用 sql.DB 连接 DuckDB
	// 我们使用原生 SQL 来执行所有操作
	// 如果需要使用 GORM，可以考虑使用 github.com/alifiroozi80/duckdb 驱动

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

// columnExists 检查表中是否存在指定列
func columnExists(db *sql.DB, tableName, columnName string) (bool, error) {
	// DuckDB 使用 PRAGMA table_info 来获取表信息
	query := `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`
	var count int
	err := db.QueryRow(query, tableName, columnName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ensureDuckDBExtensions 确保 DuckDB 扩展已加载
func ensureDuckDBExtensions(db *sql.DB) error {
	// 检查扩展是否已加载
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM duckdb_extensions() WHERE loaded = true AND extension_name IN ('fts', 'vss')").Scan(&count)
	if err != nil {
		// 如果查询失败，尝试手动加载
		logrus.Warn("Failed to check extensions, attempting to load manually")
		_, _ = db.Exec("INSTALL fts; LOAD fts;")
		_, _ = db.Exec("INSTALL vss; LOAD vss;")
		return nil
	}

	if count < 2 {
		logrus.Warn("Some extensions may not be loaded, attempting to load")
		_, _ = db.Exec("INSTALL fts; LOAD fts;")
		_, _ = db.Exec("INSTALL vss; LOAD vss;")
	}

	logrus.Info("DuckDB extensions verified")
	return nil
}

// createDuckDBFTSIndex 创建 DuckDB 全文搜索索引
func createDuckDBFTSIndex(db *sql.DB) error {
	// DuckDB 的 FTS 扩展使用 PRAGMA create_fts_index 创建索引
	// 语法：PRAGMA create_fts_index('table_name', 'id_column', 'text_column1', 'text_column2', ...)
	// 同时索引 content 和 content_tokens（sego 分词结果）字段
	createFTSSQL := `PRAGMA create_fts_index('documents', 'id', 'content', 'content_tokens');`
	_, err := db.Exec(createFTSSQL)
	if err != nil {
		// 如果索引已存在，忽略错误
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate") {
			logrus.Info("FTS index already exists")
			return nil
		}
		return fmt.Errorf("failed to create FTS index: %w", err)
	}

	logrus.Info("DuckDB FTS index created successfully with sego tokenization support")
	return nil
}

// tokenizeWithSego 使用 sego 对文本进行分词，返回用空格分隔的词
func tokenizeWithSego(text string) string {
	return sego.Tokenize(text)
}

// createDuckDBVectorIndex 创建 DuckDB 向量索引
func createDuckDBVectorIndex(db *sql.DB) error {
	// 使用 DuckDB 的 VSS 扩展创建 HNSW 向量索引
	// 注意：需要确保 embedding 列存在且类型正确
	createVectorIndexSQL := `
	CREATE INDEX IF NOT EXISTS documents_embedding_idx 
	ON documents USING hnsw (embedding);
	`
	_, err := db.Exec(createVectorIndexSQL)
	if err != nil {
		// 如果索引已存在或列不存在，记录警告但不失败
		if strings.Contains(err.Error(), "already exists") {
			logrus.Info("Vector index already exists")
			return nil
		}
		// 如果 embedding 列不存在，这是正常的（因为它是可选的）
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "column") {
			logrus.Warn("Embedding column may not exist yet, vector index will be created when needed")
			return nil
		}
		return fmt.Errorf("failed to create vector index: %w", err)
	}

	logrus.Info("DuckDB vector index created successfully")
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

	// 检查集合是否存在
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

	// 检查 embedding 列是否存在
	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column, assuming it exists")
		hasEmbedding = true // 默认假设存在，保持向后兼容
	}

	// 检查 content 列是否存在
	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true // 默认假设存在，保持向后兼容
	}

	// 构建查询 - 根据 embedding 和 content 列是否存在动态构建
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
		// DuckDB 支持 JSON 函数
		baseQuery += ` AND json_extract(data, '$.tags') LIKE ?`
		args = append(args, "%"+tagFilter+"%")
	}

	// 获取总数
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

	// 分页查询
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

	// 检查 embedding 列是否存在
	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column, assuming it exists")
		hasEmbedding = true // 默认假设存在，保持向后兼容
	}

	// 检查 content 列是否存在
	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true // 默认假设存在，保持向后兼容
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

	// 提取文本内容用于全文搜索
	content := extractTextFromData(string(dataJSON))
	// 使用 sego 对内容进行分词
	contentTokens := tokenizeWithSego(content)

	// 提取 embedding（如果存在）
	embeddingStr := ""
	if embeddingField, ok := data["embedding"]; ok {
		embeddingJSON, err := json.Marshal(embeddingField)
		if err == nil {
			embeddingStr = string(embeddingJSON)
		}
	}

	// 检查 embedding 列是否存在
	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column, assuming it exists")
		hasEmbedding = true // 默认假设存在，保持向后兼容
	}

	// 检查 content 列是否存在
	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true // 默认假设存在，保持向后兼容
	}

	// 检查 content_tokens 列是否存在
	hasContentTokens, err := columnExists(sqlDB, "documents", "content_tokens")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content_tokens column, assuming it exists")
		hasContentTokens = true // 默认假设存在，保持向后兼容
	}

	// 插入文档 - 根据 embedding、content 和 content_tokens 列是否存在动态构建
	var insertQuery string
	if hasEmbedding && hasContent && hasContentTokens {
		insertQuery = `INSERT INTO documents (id, collection_name, data, embedding, content, content_tokens, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err = sqlDB.Exec(insertQuery, id, name, string(dataJSON), embeddingStr, content, contentTokens)
	} else if hasEmbedding && hasContent && !hasContentTokens {
		insertQuery = `INSERT INTO documents (id, collection_name, data, embedding, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err = sqlDB.Exec(insertQuery, id, name, string(dataJSON), embeddingStr, content)
	} else if hasEmbedding && !hasContent && hasContentTokens {
		insertQuery = `INSERT INTO documents (id, collection_name, data, embedding, content_tokens, created_at, updated_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err = sqlDB.Exec(insertQuery, id, name, string(dataJSON), embeddingStr, contentTokens)
	} else if hasEmbedding && !hasContent && !hasContentTokens {
		insertQuery = `INSERT INTO documents (id, collection_name, data, embedding, created_at, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err = sqlDB.Exec(insertQuery, id, name, string(dataJSON), embeddingStr)
	} else if !hasEmbedding && hasContent && hasContentTokens {
		insertQuery = `INSERT INTO documents (id, collection_name, data, content, content_tokens, created_at, updated_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err = sqlDB.Exec(insertQuery, id, name, string(dataJSON), content, contentTokens)
	} else if !hasEmbedding && hasContent && !hasContentTokens {
		insertQuery = `INSERT INTO documents (id, collection_name, data, content, created_at, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err = sqlDB.Exec(insertQuery, id, name, string(dataJSON), content)
	} else if !hasEmbedding && !hasContent && hasContentTokens {
		insertQuery = `INSERT INTO documents (id, collection_name, data, content_tokens, created_at, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err = sqlDB.Exec(insertQuery, id, name, string(dataJSON), contentTokens)
	} else {
		insertQuery = `INSERT INTO documents (id, collection_name, data, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err = sqlDB.Exec(insertQuery, id, name, string(dataJSON))
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// DuckDB 的 FTS 索引会自动更新，无需手动维护

	c.JSON(http.StatusCreated, DocumentResponse{
		ID:   id,
		Data: data,
	})
}

// updateDocument 更新文档
func updateDocument(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")

	// 检查 embedding 列是否存在
	hasEmbedding, err := columnExists(sqlDB, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column, assuming it exists")
		hasEmbedding = true // 默认假设存在，保持向后兼容
	}

	// 检查 content 列是否存在
	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true // 默认假设存在，保持向后兼容
	}

	// 获取现有文档
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

	// 提取文本内容用于全文搜索
	content := extractTextFromData(string(dataJSON))
	// 使用 sego 对内容进行分词
	contentTokens := tokenizeWithSego(content)

	// 提取 embedding（如果存在）
	embeddingStr := ""
	if embeddingField, ok := data["embedding"]; ok {
		embeddingJSON, err := json.Marshal(embeddingField)
		if err == nil {
			embeddingStr = string(embeddingJSON)
		}
	}

	// 检查 content_tokens 列是否存在
	hasContentTokens, err := columnExists(sqlDB, "documents", "content_tokens")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content_tokens column, assuming it exists")
		hasContentTokens = true // 默认假设存在，保持向后兼容
	}

	// 更新文档 - 根据 embedding、content 和 content_tokens 列是否存在动态构建
	var updateQuery string
	if hasEmbedding && hasContent && hasContentTokens {
		updateQuery = `UPDATE documents SET data = ?, embedding = ?, content = ?, content_tokens = ?, updated_at = CURRENT_TIMESTAMP WHERE collection_name = ? AND id = ?`
		_, err = sqlDB.Exec(updateQuery, string(dataJSON), embeddingStr, content, contentTokens, name, id)
	} else if hasEmbedding && hasContent && !hasContentTokens {
		updateQuery = `UPDATE documents SET data = ?, embedding = ?, content = ?, updated_at = CURRENT_TIMESTAMP WHERE collection_name = ? AND id = ?`
		_, err = sqlDB.Exec(updateQuery, string(dataJSON), embeddingStr, content, name, id)
	} else if hasEmbedding && !hasContent && hasContentTokens {
		updateQuery = `UPDATE documents SET data = ?, embedding = ?, content_tokens = ?, updated_at = CURRENT_TIMESTAMP WHERE collection_name = ? AND id = ?`
		_, err = sqlDB.Exec(updateQuery, string(dataJSON), embeddingStr, contentTokens, name, id)
	} else if hasEmbedding && !hasContent && !hasContentTokens {
		updateQuery = `UPDATE documents SET data = ?, embedding = ?, updated_at = CURRENT_TIMESTAMP WHERE collection_name = ? AND id = ?`
		_, err = sqlDB.Exec(updateQuery, string(dataJSON), embeddingStr, name, id)
	} else if !hasEmbedding && hasContent && hasContentTokens {
		updateQuery = `UPDATE documents SET data = ?, content = ?, content_tokens = ?, updated_at = CURRENT_TIMESTAMP WHERE collection_name = ? AND id = ?`
		_, err = sqlDB.Exec(updateQuery, string(dataJSON), content, contentTokens, name, id)
	} else if !hasEmbedding && hasContent && !hasContentTokens {
		updateQuery = `UPDATE documents SET data = ?, content = ?, updated_at = CURRENT_TIMESTAMP WHERE collection_name = ? AND id = ?`
		_, err = sqlDB.Exec(updateQuery, string(dataJSON), content, name, id)
	} else if !hasEmbedding && !hasContent && hasContentTokens {
		updateQuery = `UPDATE documents SET data = ?, content_tokens = ?, updated_at = CURRENT_TIMESTAMP WHERE collection_name = ? AND id = ?`
		_, err = sqlDB.Exec(updateQuery, string(dataJSON), contentTokens, name, id)
	} else {
		updateQuery = `UPDATE documents SET data = ?, updated_at = CURRENT_TIMESTAMP WHERE collection_name = ? AND id = ?`
		_, err = sqlDB.Exec(updateQuery, string(dataJSON), name, id)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// DuckDB 的 FTS 索引会自动更新，无需手动维护

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

	// DuckDB 的 FTS 索引会自动更新，无需手动删除

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

	// 检查 content 列是否存在
	hasContent, err := columnExists(sqlDB, "documents", "content")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content column, assuming it exists")
		hasContent = true // 默认假设存在，保持向后兼容
	}

	// 如果 content 列不存在，使用 data 列进行搜索
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

			// 检查阈值
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

	// 检查 FTS 索引是否存在，如果不存在则尝试创建
	var indexExists bool
	// DuckDB 使用不同的系统表来检查索引
	checkSQL := `SELECT COUNT(*) FROM pragma_table_info('documents') WHERE name = 'content'`
	var count int
	if err := sqlDB.QueryRow(checkSQL).Scan(&count); err == nil && count > 0 {
		// 尝试查询 FTS 索引（DuckDB FTS 索引可能不会在常规索引表中显示）
		// 我们通过尝试创建索引来判断是否已存在
		indexExists = false // 先假设不存在，尝试创建时会处理已存在的情况
	}

	if !indexExists {
		logrus.Warn("FTS index does not exist, attempting to create it")
		if err := createDuckDBFTSIndex(sqlDB); err != nil {
			logrus.WithError(err).Error("Failed to create FTS index")
			// 不返回错误，继续使用 LIKE 查询作为回退
		}
	}

	// 使用 sego 对查询进行分词
	queryTokens := tokenizeWithSego(req.Query)

	// 检查 content_tokens 列是否存在
	hasContentTokens, err := columnExists(sqlDB, "documents", "content_tokens")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check content_tokens column, assuming it exists")
		hasContentTokens = true // 默认假设存在，保持向后兼容
	}

	// 使用 DuckDB FTS 进行全文搜索
	// 优先使用 content_tokens 字段进行搜索（sego 分词结果），如果不存在则使用 content 字段
	var query string
	var searchText string
	if hasContentTokens && queryTokens != "" {
		// 使用分词结果搜索 content_tokens 字段
		query = `
		SELECT id, collection_name, data, CAST(1.0 AS DOUBLE) as score
		FROM documents
		WHERE collection_name = ? 
		  AND content_tokens MATCH ?
		LIMIT ?
		`
		searchText = queryTokens
	} else {
		// 回退到原始 content 字段搜索
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
		// 如果 FTS 查询失败，使用 LIKE 查询作为回退
		logrus.WithError(err).Warn("FTS query failed, using LIKE query as fallback")
		// 如果 content_tokens 存在，优先在 content_tokens 中搜索
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

		// 检查阈值（注意：DuckDB FTS 的分数可能不同，这里使用简单的过滤）
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

	// 由于 embedding 存储为 JSON 字符串，我们使用内存计算进行向量搜索
	// 未来可以优化为直接在 DuckDB 中存储数组类型并使用 VSS 扩展
	// 目前使用回退方案（内存计算）
	vectorSearchFallback(c, name, req, queryVector)
}

// vectorSearchFallback 向量搜索的回退方案（内存计算）
func vectorSearchFallback(c *gin.Context, name string, req VectorSearchRequest, queryVector []float64) {
	start := time.Now()

	// 获取集合中的所有文档
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

// DuckDB 的 FTS 索引是自动维护的，无需手动重新索引

// extractTextFromData 从 JSON 数据中提取文本内容
func extractTextFromData(dataJSON string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return ""
	}

	var parts []string
	for k, v := range data {
		if k == "id" || k == "_rev" || k == "embedding" {
			continue
		}
		if str, ok := v.(string); ok {
			parts = append(parts, str)
		} else if arr, ok := v.([]interface{}); ok {
			// 处理数组字段（如 tags）
			for _, item := range arr {
				if str, ok := item.(string); ok {
					parts = append(parts, str)
				}
			}
		} else {
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	return strings.Join(parts, " ")
}

// DuckDB 的 FTS 索引是自动维护的，无需手动重新索引

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
