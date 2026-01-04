package textsearch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	sqlite3_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"golang.org/x/time/rate"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

// Document GORM 模型，表示向量存储中的文档
type Document struct {
	ID              []byte    `gorm:"type:BLOB(16);primaryKey;not null"` // UUID 二进制
	Content         string    `gorm:"type:TEXT"`
	Metadata        string    `gorm:"type:TEXT"` // JSON 格式的元数据
	Embedding       []byte    `gorm:"type:BLOB"` // JSON 格式的向量数组
	EmbeddingStatus string    `gorm:"type:TEXT;default:'pending'"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`
	Rev             int       `gorm:"column:_rev;default:1"`
}

// TableName 指定表名
func (Document) TableName() string {
	return "textsearch_documents"
}

// VecStore 基于SQLite的纯文本向量搜索存储
type VecStore struct {
	db           *gorm.DB
	tableName    string
	embedder     Embedder
	initialized  bool
	mu           sync.Mutex
	limiter      *rate.Limiter
	limiterOnce  sync.Once
	isProcessing bool
}

// New 创建VecStore实例
func New(opts Options) *VecStore {
	return &VecStore{
		embedder:  opts.Embedder,
		tableName: "textsearch_documents",
	}
}

// Initialize 初始化存储后端
// 使用 sqlite3-driver 的 SQLite 数据库和 GORM
func (v *VecStore) Initialize(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.initialized {
		return nil
	}

	// 打开SQLite数据库连接，使用 GORM
	// sqlite3-driver 会自动处理路径和 WAL 模式
	// 注意：GORM 的 sqlite 驱动使用 modernc.org/sqlite，但我们可以通过 DSN 来利用 sqlite3-driver 的特性
	db, err := gorm.Open(sqlite.Open("textsearch.db"), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	v.db = db

	// 使用 GORM AutoMigrate 创建表（如果不存在）
	// GORM 会自动处理表结构迁移
	if err := v.db.AutoMigrate(&Document{}); err != nil {
		return fmt.Errorf("failed to migrate table: %w", err)
	}

	// 清理无效数据：删除 id 为 NULL 或长度不为 16 的记录
	if err := v.db.WithContext(ctx).Where("id IS NULL OR length(id) != 16").Delete(&Document{}).Error; err != nil {
		// 记录错误但不阻止初始化
		log.Printf("[Initialize] 清理无效数据时出错: %v", err)
	} else {
		log.Printf("[Initialize] 已清理无效数据（id 为 NULL 或长度不为 16 的记录）")
	}

	// 创建索引以提高查询性能
	// GORM 可以通过标签自动创建索引，但这里我们手动创建以确保兼容性
	if err := v.db.Exec(fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_embedding_status_idx 
		ON %s (embedding_status)
	`, v.tableName, v.tableName)).Error; err != nil {
		log.Printf("[Initialize] 创建索引时出错: %v", err)
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

	// 使用 UUID 生成主键（二进制形式）
	uuidVal := uuid.New()
	idBytes := uuidVal[:]     // UUID 本身就是 [16]byte
	idStr := uuidVal.String() // 用于在 content JSON 中存储

	// 构建文档
	doc := map[string]any{
		"id":         idStr,
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

	// 插入到数据库，使用 GORM Create
	// 使用 UUID 二进制作为主键，确保唯一性
	document := Document{
		ID:              idBytes,
		Content:         string(contentJSON),
		Metadata:        metadataJSON,
		EmbeddingStatus: "pending",
		Rev:             1,
	}

	if err := v.db.WithContext(ctx).Create(&document).Error; err != nil {
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

	// 使用 GORM 查询所有候选文档（带metadata过滤）
	var documents []Document
	dbQuery := v.db.WithContext(ctx).Model(&Document{}).
		Where("embedding IS NOT NULL AND embedding_status = ?", "completed")

	// 添加metadata过滤条件
	if metadataFilter != nil && len(metadataFilter) > 0 {
		for key, value := range metadataFilter {
			// 转义key以防止SQL注入
			escapedKey := strings.ReplaceAll(key, "'", "''")
			// SQLite 使用 json_extract 函数
			condition := fmt.Sprintf(
				"(json_extract(COALESCE(metadata, '{}'), '$.%s') = ? OR json_extract(content, '$.%s') = ?)",
				escapedKey, escapedKey,
			)

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
			dbQuery = dbQuery.Where(condition, valueStr, valueStr)
		}
	}

	if err := dbQuery.Find(&documents).Error; err != nil {
		return nil, fmt.Errorf("failed to search vectors: %w", err)
	}

	// 在内存中计算余弦相似度并排序
	type candidateResult struct {
		id         string
		content    string
		metadata   any
		doc        map[string]any
		similarity float64
	}

	var candidates []candidateResult
	for _, doc := range documents {
		// 将二进制 UUID 转换为字符串
		id, err := sqlite3_driver.BytesToUUIDString(doc.ID)
		if err != nil {
			log.Printf("[Search] 无效的 UUID 二进制数据: %v", err)
			continue
		}

		// 解析存储的向量（JSON格式）
		var storedEmbedding []float64
		if err := json.Unmarshal(doc.Embedding, &storedEmbedding); err != nil {
			continue
		}

		// 计算余弦相似度
		similarity := cosineSimilarity(queryEmbedding, storedEmbedding)

		var docMap map[string]any
		if err := json.Unmarshal([]byte(doc.Content), &docMap); err != nil {
			docMap = map[string]any{"id": id, "content": doc.Content}
		}

		var metadataVal any
		if doc.Metadata != "" {
			json.Unmarshal([]byte(doc.Metadata), &metadataVal)
		}

		candidates = append(candidates, candidateResult{
			id:         id,
			content:    doc.Content,
			metadata:   metadataVal,
			doc:        docMap,
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
		// 提取文本内容
		textContent := ""
		if text, ok := cand.doc["content"].(string); ok {
			textContent = text
		}

		results[i] = SearchResult{
			ID:      cand.id,
			Content: textContent,
			Score:   cand.similarity,
			Data:    cand.doc,
		}
	}

	return results, nil
}

// Close 关闭VecStore
func (v *VecStore) Close() error {
	if v.db != nil {
		sqlDB, err := v.db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// GetDB 返回内部的数据库连接（用于需要直接访问数据库的场景）
func (v *VecStore) GetDB() *gorm.DB {
	return v.db
}

// GetSQLDB 返回底层的 *sql.DB 连接（用于需要直接访问 SQL 的场景）
func (v *VecStore) GetSQLDB() (*sql.DB, error) {
	if v.db != nil {
		return v.db.DB()
	}
	return nil, fmt.Errorf("database not initialized")
}

// GetTableName 返回表名
func (v *VecStore) GetTableName() string {
	return v.tableName
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
