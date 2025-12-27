//go:build ignore
// +build ignore

package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

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

// 产品数据模板
var (
	categories = []string{"electronics", "clothing", "books", "home", "sports", "toys", "food", "beauty"}
	brands     = []string{"Apple", "Samsung", "Nike", "Adidas", "Sony", "Canon", "Dell", "HP", "Lenovo", "Xiaomi"}
	adjectives = []string{"高级", "专业", "经典", "时尚", "智能", "高性能", "优质", "创新", "精致", "耐用"}
	nouns      = []string{"产品", "设备", "工具", "系统", "解决方案", "套装", "系列", "型号"}
)

// generateProductData 生成产品数据
func generateProductData(id int) map[string]any {
	rand.Seed(time.Now().UnixNano() + int64(id))

	category := categories[rand.Intn(len(categories))]
	brand := brands[rand.Intn(len(brands))]
	adjective := adjectives[rand.Intn(len(adjectives))]
	noun := nouns[rand.Intn(len(nouns))]

	name := fmt.Sprintf("%s %s %s %d", brand, adjective, noun, id)
	description := fmt.Sprintf("%s %s，型号 %d，%s类别产品，具有出色的性能和品质", brand, adjective, id, category)

	return map[string]any{
		"id":          fmt.Sprintf("prod-%05d", id),
		"name":        name,
		"category":    category,
		"description": description,
	}
}

func main() {
	const totalProducts = 10000

	// 从环境变量读取数据库配置
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
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		logrus.WithError(err).Fatal("Failed to create data directory")
	}
	fmt.Println("   ✅ 数据目录已准备就绪")
	fmt.Println()

	// 初始化 SQLite3 数据库（使用 GORM）
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
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

	fmt.Printf("🚀 开始生成 %d 条产品数据用于性能测试...\n\n", totalProducts)

	// largeseed 用于性能测试，不需要生成真实的 embedding
	// 使用随机向量或直接跳过 embedding 生成以加快速度
	fmt.Println("ℹ️  性能测试模式：跳过 embedding 生成，仅生成产品数据")

	// 批量生成和插入数据
	const batchSize = 100
	const concurrency = 10 // 并发插入数量（不需要调用 API，可以提高并发）

	fmt.Printf("📊 配置: 批量大小=%d, 并发数=%d\n\n", batchSize, concurrency)

	startTime := time.Now()
	successCount := int64(0)
	errorCount := int64(0)

	// 使用 channel 控制并发
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 1; i <= totalProducts; i++ {
		product := generateProductData(i)

		// 生成 embedding
		wg.Add(1)
		semaphore <- struct{}{} // 获取信号量

		productIdx := i // 创建局部变量副本，避免并发问题
		go func(prod map[string]any, idx int) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放信号量

			// largeseed 不需要生成 embedding，直接插入数据
			// 如果需要测试向量搜索，可以使用 seed 命令生成带 embedding 的数据

			// 将数据序列化为 JSON
			dataJSON, err := json.Marshal(prod)
			if err != nil {
				mu.Lock()
				errorCount++
				logrus.WithError(err).WithField("product_id", prod["id"]).Error("序列化失败")
				mu.Unlock()
				return
			}

			doc := Document{
				ID:             prod["id"].(string),
				CollectionName: "products",
				Data:           string(dataJSON),
			}

			// 插入数据库
			err = gormDB.Create(&doc).Error
			mu.Lock()
			if err != nil {
				errorCount++
				logrus.WithError(err).WithField("product_id", prod["id"]).Error("插入失败")
			} else {
				successCount++
				if idx%100 == 0 {
					elapsed := time.Since(startTime)
					rate := float64(successCount) / elapsed.Seconds()
					remaining := float64(totalProducts-int(successCount)) / rate
					fmt.Printf("  ✅ 进度: %d/%d (%.1f%%) | 成功: %d | 失败: %d | 速度: %.1f 条/秒 | 预计剩余: %.0f 秒\n",
						idx, totalProducts, float64(idx)/float64(totalProducts)*100,
						successCount, errorCount, rate, remaining)
				}
			}
			mu.Unlock()
		}(product, productIdx)

		// 每批完成后稍作休息，避免过载
		if i%batchSize == 0 {
			wg.Wait()                          // 等待当前批次完成
			time.Sleep(100 * time.Millisecond) // 短暂休息
		}
	}

	// 等待所有任务完成
	wg.Wait()

	elapsed := time.Since(startTime)
	fmt.Printf("\n✨ 数据生成完成！\n")
	fmt.Printf("   - 总计: %d 条\n", totalProducts)
	fmt.Printf("   - 成功: %d 条\n", successCount)
	logrus.WithField("error_count", errorCount).Info("失败记录数")
	fmt.Printf("   - 耗时: %v\n", elapsed.Round(time.Second))
	fmt.Printf("   - 平均速度: %.1f 条/秒\n", float64(successCount)/elapsed.Seconds())

	// 统计信息
	var productsCount int64
	gormDB.Model(&Document{}).Where("collection_name = ?", "products").Count(&productsCount)
	fmt.Printf("\n📊 数据库统计:\n")
	fmt.Printf("   - products: %d 个\n", productsCount)
	fmt.Println("\n💡 提示:")
	fmt.Println("  - 在浏览器中访问 http://localhost:40111 查看数据")
	fmt.Println("  - 使用 'products' 集合测试文档查询和分页性能")
	fmt.Println("  - 注意: 此数据不包含 embedding，如需测试向量搜索，请使用 'make seed' 命令")
}
