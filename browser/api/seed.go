//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Document 文档模型（与 main.go 保持一致）
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

// generateEmbedding 使用 DashScope API 生成文本的 embedding 向量
func generateEmbedding(text string) ([]float64, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY environment variable is not set")
	}

	// DashScope embedding API 端点
	url := "https://dashscope.aliyuncs.com/api/v1/services/embeddings/text-embedding/text-embedding"

	// 构建请求
	reqBody := DashScopeEmbeddingRequest{
		Model: "text-embedding-v4", // DashScope 文本嵌入模型 v4
		Input: DashScopeInput{
			Texts: []string{text},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 创建 HTTP 请求
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	// 发送请求
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var apiResp DashScopeEmbeddingResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResp.Output.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	// 将 embedding 转换为 []float64
	embedding := apiResp.Output.Embeddings[0].Embedding
	result := make([]float64, len(embedding))
	for i, v := range embedding {
		result[i] = float64(v)
	}

	return result, nil
}

// generateCategoryEmbedding 基于分类信息生成 embedding
func generateCategoryEmbedding(category, subcategory, description string) []float64 {
	// 组合文本用于生成 embedding
	text := strings.Join([]string{category, subcategory, description}, " ")

	embedding, err := generateEmbedding(text)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"category":    category,
			"subcategory": subcategory,
		}).Warn("生成 embedding 失败，使用随机向量")
		// 回退到随机向量
		return generateRandomEmbedding(1536) // DashScope 默认维度是 1536
	}

	return embedding
}

// generateRandomEmbedding 生成随机向量（作为回退方案）
func generateRandomEmbedding(dim int) []float64 {
	embedding := make([]float64, dim)
	for i := range embedding {
		embedding[i] = float64(i%100) / 100.0 // 简单的伪随机
	}
	return embedding
}

// createFTS5Table 创建 FTS5 全文搜索虚拟表
func createFTS5Table(db *sql.DB) error {
	createFTS5SQL := `
	CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
		id UNINDEXED,
		collection_name,
		content,
		content_rowid=id
	);
	`
	_, err := db.Exec(createFTS5SQL)
	return err
}

// updateFTS5Index 更新 FTS5 索引
func updateFTS5Index(db *sql.DB, doc Document) {
	// 提取文本内容用于全文搜索
	content := extractTextFromData(doc.Data)

	// 使用 INSERT OR REPLACE 更新 FTS5 索引
	query := `
	INSERT OR REPLACE INTO documents_fts(id, collection_name, content)
	VALUES (?, ?, ?)
	`
	_, err := db.Exec(query, doc.ID, doc.CollectionName, content)
	if err != nil {
		logrus.WithError(err).Warn("Failed to update FTS5 index")
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

func main() {
	// 从环境变量读取数据库配置（与 API 服务器保持一致）
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "browser-db"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/browser-db"
	}

	// 删除旧的数据目录（如果存在）
	fmt.Println("🗑️  清理旧数据目录...")
	if _, err := os.Stat(dbPath); err == nil {
		fmt.Printf("   删除目录: %s\n", dbPath)
		if err := os.RemoveAll(dbPath); err != nil {
			logrus.WithError(err).Fatal("Failed to remove old data directory")
		}
		fmt.Println("   ✅ 旧数据目录已删除")
	} else if os.IsNotExist(err) {
		fmt.Println("   ℹ️  数据目录不存在，跳过删除")
	} else {
		logrus.WithError(err).Fatal("Failed to check data directory")
	}

	// 确保数据目录存在
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		logrus.WithError(err).Fatal("Failed to create data directory")
	}
	fmt.Println("   ✅ 数据目录已准备就绪")
	fmt.Println()

	ctx := context.Background()

	// 初始化 SQLite3 数据库（使用 GORM）
	// SQLite 需要文件路径，而不是目录路径
	sqliteDBPath := filepath.Join(dbPath, "browser.db")
	gormDB, err := gorm.Open(sqlite.Open(sqliteDBPath), &gorm.Config{})
	if err != nil {
		logrus.WithError(err).Fatal("Failed to connect database")
	}

	// 获取底层 sql.DB
	sqlDB, err := gormDB.DB()
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get database instance")
	}
	defer sqlDB.Close()

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
	graphDB, err := cayley_driver.NewGraph(graphDBPath)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create graph database")
	}
	defer graphDB.Close()

	fmt.Println("🌱 开始生成示例数据...")
	fmt.Println()

	// ========================================
	// 创建 articles 集合（用于全文搜索）
	// ========================================
	fmt.Println("📚 创建 articles 集合...")

	articles := []map[string]any{
		{
			"id":      "article-001",
			"title":   "Go 语言入门指南",
			"content": "Go 是一种静态类型、编译型语言，由 Google 开发。它具有简洁的语法和强大的并发支持，非常适合构建高性能的服务端应用程序。Go 语言的设计哲学是简洁、高效和可读性强。",
			"author":  "张三",
			"tags":    []string{"Go", "编程", "入门"},
		},
		{
			"id":      "article-002",
			"title":   "深入理解 Go 并发编程",
			"content": "Go 的 goroutine 和 channel 是其并发模型的核心。通过 goroutine 可以轻松创建轻量级线程，而 channel 则提供了安全的通信方式。这种设计使得编写并发程序变得简单而优雅。",
			"author":  "李四",
			"tags":    []string{"Go", "并发", "高级"},
		},
		{
			"id":      "article-003",
			"title":   "Python 机器学习实战",
			"content": "Python 是数据科学和机器学习的首选语言。本文介绍如何使用 scikit-learn 和 TensorFlow 构建机器学习模型。从数据预处理到模型训练，全面覆盖机器学习工作流程。",
			"author":  "王五",
			"tags":    []string{"Python", "机器学习", "AI"},
		},
		{
			"id":      "article-004",
			"title":   "JavaScript 前端框架对比",
			"content": "React、Vue 和 Angular 是目前最流行的前端框架。本文将从性能、学习曲线和生态系统等方面进行详细对比，帮助开发者选择最适合的框架。",
			"author":  "赵六",
			"tags":    []string{"JavaScript", "前端", "框架"},
		},
		{
			"id":      "article-005",
			"title":   "Go 微服务架构设计",
			"content": "微服务架构已成为现代应用开发的主流模式。Go 语言凭借其出色的性能和简单的部署方式，成为微服务开发的热门选择。本文将介绍如何设计可扩展的微服务系统。",
			"author":  "张三",
			"tags":    []string{"Go", "微服务", "架构"},
		},
		{
			"id":      "article-006",
			"title":   "数据库设计最佳实践",
			"content": "良好的数据库设计是应用成功的基础。本文介绍关系型数据库和 NoSQL 数据库的设计原则，包括索引优化、查询性能调优和数据结构选择等关键话题。",
			"author":  "李四",
			"tags":    []string{"数据库", "设计", "优化"},
		},
		{
			"id":      "article-007",
			"title":   "容器化部署指南",
			"content": "Docker 和 Kubernetes 是现代应用部署的标准工具。本文详细介绍如何使用容器技术打包、部署和管理应用程序，包括最佳实践和常见问题解决方案。",
			"author":  "王五",
			"tags":    []string{"Docker", "Kubernetes", "部署"},
		},
		{
			"id":      "article-008",
			"title":   "RESTful API 设计规范",
			"content": "RESTful API 是 Web 服务的主流架构风格。本文介绍 REST API 的设计原则、HTTP 方法的使用、状态码的选择以及版本控制策略，帮助开发者构建高质量的 API。",
			"author":  "赵六",
			"tags":    []string{"API", "REST", "设计"},
		},
	}

	fmt.Printf("  插入 %d 篇文章...\n", len(articles))
	for i, article := range articles {
		// 将数据序列化为 JSON
		dataJSON, err := json.Marshal(article)
		if err != nil {
			logrus.WithError(err).WithField("article_id", article["id"]).Error("序列化失败")
			continue
		}

		doc := Document{
			ID:             article["id"].(string),
			CollectionName: "articles",
			Data:           string(dataJSON),
		}

		if err := gormDB.Create(&doc).Error; err != nil {
			logrus.WithError(err).WithField("article_id", article["id"]).Error("插入失败")
		} else {
			fmt.Printf("  ✅ [%d/%d] %s\n", i+1, len(articles), article["id"])
			// 更新 FTS5 索引
			updateFTS5Index(sqlDB, doc)
		}
	}
	fmt.Printf("✅ articles 集合创建完成，共 %d 篇文章\n\n", len(articles))

	// ========================================
	// 创建 products 集合（用于向量搜索）
	// ========================================
	fmt.Println("🛒 创建 products 集合...")

	products := []map[string]any{
		{
			"id":          "prod-001",
			"name":        "iPhone 15 Pro",
			"category":    "electronics",
			"description": "Apple 旗舰智能手机，搭载 A17 Pro 芯片",
		},
		{
			"id":          "prod-002",
			"name":        "Samsung Galaxy S24",
			"category":    "electronics",
			"description": "三星旗舰智能手机，搭载 AI 功能",
		},
		{
			"id":          "prod-003",
			"name":        "MacBook Pro 16",
			"category":    "electronics",
			"description": "Apple 专业笔记本电脑，M3 Max 芯片",
		},
		{
			"id":          "prod-004",
			"name":        "Nike Air Max",
			"category":    "clothing",
			"description": "经典运动鞋，舒适透气",
		},
		{
			"id":          "prod-005",
			"name":        "Adidas Ultraboost",
			"category":    "clothing",
			"description": "高性能跑步鞋，Boost 中底",
		},
		{
			"id":          "prod-006",
			"name":        "Levi's 501 牛仔裤",
			"category":    "clothing",
			"description": "经典直筒牛仔裤",
		},
		{
			"id":          "prod-007",
			"name":        "《深入理解计算机系统》",
			"category":    "books",
			"description": "计算机科学经典教材",
		},
		{
			"id":          "prod-008",
			"name":        "《三体》",
			"category":    "books",
			"description": "刘慈欣科幻小说代表作",
		},
		{
			"id":          "prod-009",
			"name":        "iPad Pro",
			"category":    "electronics",
			"description": "Apple 专业平板电脑，M2 芯片",
		},
		{
			"id":          "prod-010",
			"name":        "AirPods Pro",
			"category":    "electronics",
			"description": "Apple 主动降噪无线耳机",
		},
	}

	fmt.Printf("  插入 %d 个产品...\n", len(products))
	fmt.Println("  ⚠️  正在使用 DashScope 生成 embedding，这可能需要一些时间...")

	// 检查是否设置了 API Key
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		logrus.Warn("DASHSCOPE_API_KEY 未设置，将使用随机向量")
		logrus.Info("提示: 设置环境变量 DASHSCOPE_API_KEY 以使用真实的 embedding")
	}

	for i, product := range products {
		// 为每个产品生成 embedding
		name := product["name"].(string)
		description := product["description"].(string)
		category := product["category"].(string)
		text := fmt.Sprintf("%s %s %s", name, category, description)

		fmt.Printf("  🔄 [%d/%d] 正在为 %s 生成 embedding...\n", i+1, len(products), name)
		embedding, err := generateEmbedding(text)
		if err != nil {
			logrus.WithError(err).WithField("product_id", product["id"]).Warn("生成 embedding 失败，使用随机向量")
			embedding = generateRandomEmbedding(1536)
		}

		// 验证 embedding 维度
		if len(embedding) == 0 {
			logrus.Warn("embedding dimension is 0")
			embedding = generateRandomEmbedding(1536)
		} else {
			logrus.WithField("dimension", len(embedding)).Debug("Generated embedding dimension (text-embedding-v4)")
		}

		product["embedding"] = embedding

		// 将数据序列化为 JSON
		dataJSON, err := json.Marshal(product)
		if err != nil {
			logrus.WithError(err).WithField("product_id", product["id"]).Error("序列化失败")
			continue
		}

		doc := Document{
			ID:             product["id"].(string),
			CollectionName: "products",
			Data:           string(dataJSON),
		}

		if err := gormDB.Create(&doc).Error; err != nil {
			logrus.WithError(err).WithField("product_id", product["id"]).Error("插入失败")
		} else {
			fmt.Printf("  ✅ [%d/%d] %s (embedding 维度: %d)\n", i+1, len(products), product["id"], len(embedding))
		}
	}
	fmt.Printf("✅ products 集合创建完成，共 %d 个产品\n\n", len(products))

	// ========================================
	// 创建 users 集合（用于图数据库）
	// ========================================
	fmt.Println("👥 创建 users 集合...")

	users := []map[string]any{
		{
			"id":      "user1",
			"name":    "Alice",
			"email":   "alice@example.com",
			"follows": []string{"user2", "user3"},
		},
		{
			"id":      "user2",
			"name":    "Bob",
			"email":   "bob@example.com",
			"follows": []string{"user3", "user4"},
		},
		{
			"id":      "user3",
			"name":    "Charlie",
			"email":   "charlie@example.com",
			"follows": []string{"user4"},
		},
		{
			"id":      "user4",
			"name":    "Diana",
			"email":   "diana@example.com",
			"follows": []string{"user1"},
		},
		{
			"id":      "user5",
			"name":    "Eve",
			"email":   "eve@example.com",
			"follows": []string{"user1", "user2"},
		},
	}

	fmt.Printf("  插入 %d 个用户...\n", len(users))
	for i, user := range users {
		// 将数据序列化为 JSON
		dataJSON, err := json.Marshal(user)
		if err != nil {
			logrus.WithError(err).WithField("user_id", user["id"]).Error("序列化失败")
			continue
		}

		doc := Document{
			ID:             user["id"].(string),
			CollectionName: "users",
			Data:           string(dataJSON),
		}

		if err := gormDB.Create(&doc).Error; err != nil {
			logrus.WithError(err).WithField("user_id", user["id"]).Error("插入失败")
		} else {
			fmt.Printf("  ✅ [%d/%d] %s\n", i+1, len(users), user["id"])
		}
	}
	fmt.Printf("✅ users 集合创建完成，共 %d 个用户\n\n", len(users))

	// ========================================
	// 创建图关系（关注关系）
	// ========================================
	fmt.Println("🔗 创建图关系...")
	fmt.Println("  ✅ 图数据库已初始化")

	// 创建关注关系
	relations := []struct {
		from     string
		relation string
		to       string
	}{
		{"user1", "follows", "user2"},
		{"user1", "follows", "user3"},
		{"user2", "follows", "user3"},
		{"user2", "follows", "user4"},
		{"user3", "follows", "user4"},
		{"user4", "follows", "user1"},
		{"user5", "follows", "user1"},
		{"user5", "follows", "user2"},
	}

	fmt.Printf("  创建 %d 个关注关系...\n", len(relations))
	successCount := 0
	for i, rel := range relations {
		if err := graphDB.Link(ctx, rel.from, rel.relation, rel.to); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"index":    i + 1,
				"total":    len(relations),
				"from":     rel.from,
				"relation": rel.relation,
				"to":       rel.to,
			}).Error("创建关系失败")
		} else {
			fmt.Printf("  ✅ [%d/%d] %s --%s--> %s\n", i+1, len(relations), rel.from, rel.relation, rel.to)
			successCount++
		}
	}
	fmt.Printf("✅ 图关系创建完成，成功创建 %d/%d 个关系\n\n", successCount, len(relations))

	// 验证图关系是否创建成功
	fmt.Println("🔍 验证图关系...")
	testNeighbors, err := graphDB.GetNeighbors(ctx, "user1", "follows")
	if err != nil {
		logrus.WithError(err).Warn("验证失败")
	} else {
		fmt.Printf("  ✅ user1 的邻居: %v\n", testNeighbors)
		if len(testNeighbors) == 0 {
			logrus.Warn("user1 没有邻居，图关系可能没有正确创建")
		}
	}
	fmt.Println()

	// ========================================
	// 统计信息
	// ========================================
	var articlesCount, productsCount, usersCount int64
	gormDB.Model(&Document{}).Where("collection_name = ?", "articles").Count(&articlesCount)
	gormDB.Model(&Document{}).Where("collection_name = ?", "products").Count(&productsCount)
	gormDB.Model(&Document{}).Where("collection_name = ?", "users").Count(&usersCount)

	fmt.Println("📊 数据统计:")
	fmt.Printf("  - articles: %d 篇\n", articlesCount)
	fmt.Printf("  - products: %d 个\n", productsCount)
	fmt.Printf("  - users: %d 个\n", usersCount)
	fmt.Println("\n✨ 示例数据生成完成！")
	fmt.Println("\n💡 提示:")
	fmt.Println("  - 在浏览器中访问 http://localhost:40111 查看数据")
	fmt.Println("  - 使用 'articles' 集合测试全文搜索")
	fmt.Println("  - 使用 'products' 集合测试向量搜索")
	fmt.Println("  - 使用 'users' 集合和图数据库测试图查询")
}
