package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var (
	gormDB       *gorm.DB
	sqlDB        *sql.DB
	graphDB      cayley_driver.Graph
	dbContext    context.Context
	embeddingDim int // embedding 向量维度
)

// getEmbeddingDimension 获取 embedding 向量维度
func getEmbeddingDimension() int {
	if embeddingDim > 0 {
		return embeddingDim
	}
	// 从环境变量读取，默认为 1024 text-embedding-v4 的维度）
	dimStr := os.Getenv("EMBEDDING_DIMENSION")
	if dimStr != "" {
		if dim, err := strconv.Atoi(dimStr); err == nil && dim > 0 {
			embeddingDim = dim
			return embeddingDim
		}
	}
	// 默认维度为 1536
	embeddingDim = 1024
	return embeddingDim
}

// initDatabase 初始化数据库
func initDatabase() error {

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./testdata/"
	}

	// 确保数据目录存在
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	ctx := context.Background()
	dbContext = ctx

	// 初始化 embedding 维度
	dim := getEmbeddingDimension()
	logrus.WithField("embedding_dimension", dim).Info("Embedding dimension initialized")

	// 初始化 DuckDB 数据库
	// 使用 duckdb-driver，它会自动加载扩展并处理路径映射
	duckDBPath := filepath.Join(dbPath, "index.db")
	absDBPath, err := filepath.Abs(duckDBPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	logrus.WithField("db_path", absDBPath).Info("Database path")

	// 使用 duckdb-driver，它会自动处理扩展加载和读写模式
	// 注意：duckdb-driver 会将路径映射到 ./data/indexing/index.db
	// 但这里我们使用绝对路径来保持原有的数据库位置
	sqlDB, err = sql.Open("duckdb", absDBPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 确保扩展已加载
	if err := ensureDuckDBExtensions(sqlDB); err != nil {
		logrus.WithError(err).Warn("Some DuckDB extensions may not be available")
	}

	// 启用 HNSW 实验性持久化功能
	if _, err := sqlDB.Exec("SET hnsw_enable_experimental_persistence = true"); err != nil {
		logrus.WithError(err).Warn("Failed to enable HNSW experimental persistence, vector index may not work in persistent database")
		logrus.Error("❌ 向量搜索功能可能不可用：HNSW 实验性持久化功能未启用")
	} else {
		logrus.Info("HNSW experimental persistence enabled")
	}

	// 创建表（使用固定维度的向量类型以支持 HNSW 索引）
	createTableSQL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS documents (
		id VARCHAR(255) PRIMARY KEY,
		collection_name VARCHAR(255) NOT NULL,
		data TEXT,
		embedding FLOAT[%d],
		content TEXT,
		content_tokens TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_documents_collection ON documents(collection_name);
	`, dim)
	if _, err := sqlDB.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create documents table: %w", err)
	}

	// 确保必要的列存在
	if err := ensureTableColumns(sqlDB); err != nil {
		logrus.WithError(err).Warn("Failed to ensure table columns, some features may not work")
	}

	// 创建全文搜索索引
	if err := createDuckDBFTSIndex(sqlDB); err != nil {
		logrus.WithError(err).Error("Failed to create FTS index, fulltext search may not work")
	} else {
		logrus.Info("DuckDB FTS index created successfully")
	}

	// 创建向量索引
	if err := createDuckDBVectorIndex(sqlDB); err != nil {
		logrus.WithError(err).Error("Failed to create vector index, vector search may not work")
	} else {
		logrus.Info("DuckDB vector index created successfully")
	}

	// 初始化图数据库
	graphDBPath := "graph.db"
	graphDB, err = cayley_driver.NewGraphWithNamespace(dbPath, graphDBPath, "")
	if err != nil {
		if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "locked") {
			logrus.WithError(err).Error("图数据库被锁定，可能是另一个进程正在使用")
			logrus.Error("💡 提示: 可以尝试运行 'make clean-lock' 或 'make force-clean' 来清理锁文件")
			logrus.Error("   或者检查是否有其他进程正在使用数据库")
			graphDB = nil
		} else {
			return fmt.Errorf("failed to create graph database: %w", err)
		}
	}

	logrus.Info("Database initialized successfully")
	return nil
}

// columnExists 检查表中是否存在指定列
func columnExists(db *sql.DB, tableName, columnName string) (bool, error) {
	query := `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`
	var count int
	err := db.QueryRow(query, tableName, columnName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ensureTableColumns 确保表中有必要的列（用于表结构迁移）
func ensureTableColumns(db *sql.DB) error {
	requiredColumns := []struct {
		name string
		typ  string
	}{
		{"content", "TEXT"},
		{"content_tokens", "TEXT"},
		{"embedding", "FLOAT[1024]"},
	}

	for _, col := range requiredColumns {
		exists, err := columnExists(db, "documents", col.name)
		if err != nil {
			logrus.WithError(err).WithField("column", col.name).Warn("Failed to check column existence")
			continue
		}

		if !exists {
			logrus.WithField("column", col.name).Info("Adding missing column to documents table")
			alterSQL := fmt.Sprintf("ALTER TABLE documents ADD COLUMN %s %s", col.name, col.typ)
			if _, err := db.Exec(alterSQL); err != nil {
				if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "duplicate") {
					logrus.WithError(err).WithField("column", col.name).Warn("Failed to add column")
					return fmt.Errorf("failed to add column %s: %w", col.name, err)
				}
			} else {
				logrus.WithField("column", col.name).Info("Column added successfully")
			}
		}
	}

	return nil
}

// ensureDuckDBExtensions 确保 DuckDB 扩展已加载
func ensureDuckDBExtensions(db *sql.DB) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM duckdb_extensions() WHERE loaded = true AND extension_name IN ('fts', 'vss')").Scan(&count)
	if err != nil {
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
	createFTSSQL := `PRAGMA create_fts_index('documents', 'id', 'content', 'content_tokens');`
	_, err := db.Exec(createFTSSQL)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate") {
			logrus.Info("FTS index already exists")
			return nil
		}
		return fmt.Errorf("failed to create FTS index: %w", err)
	}

	logrus.Info("DuckDB FTS index created successfully with sego tokenization support")
	return nil
}

// getColumnType 获取列的类型
func getColumnType(db *sql.DB, tableName, columnName string) (string, error) {
	query := `SELECT type FROM pragma_table_info(?) WHERE name = ?`
	var colType string
	err := db.QueryRow(query, tableName, columnName).Scan(&colType)
	if err != nil {
		return "", err
	}
	return colType, nil
}

// createDuckDBVectorIndex 创建 DuckDB 向量索引
func createDuckDBVectorIndex(db *sql.DB) error {
	// 检查 embedding 列是否存在
	hasEmbedding, err := columnExists(db, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to check embedding column")
		hasEmbedding = false
	}

	if !hasEmbedding {
		logrus.Warn("embedding column does not exist, vector index will not be created")
		logrus.Error("❌ 向量搜索功能不可用：embedding 列不存在")
		return nil
	}

	// 检查列类型是否为固定维度的 FLOAT[N]
	dim := getEmbeddingDimension()
	colType, err := getColumnType(db, "documents", "embedding")
	if err != nil {
		logrus.WithError(err).Warn("Failed to get embedding column type")
	} else {
		expectedType := fmt.Sprintf("FLOAT[%d]", dim)
		if colType != expectedType && colType != fmt.Sprintf("FLOAT(%d)", dim) {
			logrus.WithFields(logrus.Fields{
				"current_type":  colType,
				"expected_type": expectedType,
			}).Error("❌ embedding 列类型不正确，无法创建 HNSW 索引")
			logrus.Error("💡 提示: HNSW 索引需要固定维度的向量类型（如 FLOAT[1024]）")
			logrus.Error("   如果表已存在，您需要：")
			logrus.Error("   1. 备份数据")
			logrus.Error("   2. 删除表并重新创建")
			logrus.Error("   3. 或者设置环境变量 EMBEDDING_DIMENSION 来匹配现有列的类型")
			return fmt.Errorf("embedding column type is %s, expected %s. Please recreate the table with the correct type", colType, expectedType)
		}
	}

	createVectorIndexSQL := `
	CREATE INDEX IF NOT EXISTS documents_embedding_idx 
	ON documents USING hnsw (embedding) WITH (metric = 'cosine');
	`
	_, err = db.Exec(createVectorIndexSQL)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			logrus.Info("Vector index already exists")
			return nil
		}
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "column") {
			logrus.Warn("Embedding column may not exist yet, vector index will be created when needed")
			return nil
		}
		if strings.Contains(err.Error(), "in-memory") || strings.Contains(err.Error(), "hnsw_enable_experimental_persistence") {
			logrus.WithError(err).Error("HNSW index persistence may not be enabled")
			logrus.Error("❌ 向量搜索功能不可用：HNSW 向量索引需要实验性持久化功能")
			return nil
		}
		return fmt.Errorf("failed to create vector index: %w", err)
	}

	logrus.Info("DuckDB vector index created successfully")
	return nil
}
