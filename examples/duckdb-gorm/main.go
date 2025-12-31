package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/alifiroozi80/duckdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// User 用户模型
type User struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string    `gorm:"type:varchar(100);not null"`
	Email     string    `gorm:"type:varchar(100);uniqueIndex;not null"`
	Age       int       `gorm:"type:integer"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// Product 产品模型
type Product struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string    `gorm:"type:varchar(100);not null"`
	Price     float64   `gorm:"type:double"`
	Stock     int       `gorm:"type:integer"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// createDBConnection 创建新的数据库连接
func createDBConnection(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(duckdb.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // 启用 SQL 日志
	})
	if err != nil {
		return nil, err
	}

	// 获取底层 sql.DB 以设置连接池参数
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// checkFulltextIndexSupport 检查数据库是否支持全文索引
func checkFulltextIndexSupport(db *gorm.DB) {
	// 方法1: 尝试安装 fts 扩展
	err := db.Exec("INSTALL fts").Error
	if err != nil {
		log.Printf("⚠️  安装 fts 扩展失败: %v", err)
	} else {
		fmt.Println("✅ fts 扩展安装成功（或已安装）")
	}

	// 方法2: 尝试加载 fts 扩展
	err = db.Exec("LOAD fts").Error
	if err != nil {
		fmt.Printf("❌ 加载 fts 扩展失败: %v\n", err)
		fmt.Println("❌ 全文索引不支持")
		return
	}
	fmt.Println("✅ fts 扩展加载成功")

	// 方法3: 检查扩展是否可用（通过查询已加载的扩展）
	var extensions []struct {
		ExtensionName string `gorm:"column:extension_name"`
		Loaded        bool   `gorm:"column:loaded"`
	}
	err = db.Raw("SELECT extension_name, loaded FROM duckdb_extensions() WHERE extension_name = 'fts'").Scan(&extensions).Error
	if err != nil {
		log.Printf("⚠️  查询扩展信息失败: %v", err)
		fmt.Println("⚠️  无法确认全文索引支持状态")
	} else {
		if len(extensions) > 0 && extensions[0].Loaded {
			fmt.Println("✅ 全文索引支持已确认")
		} else {
			fmt.Println("⚠️  fts 扩展未加载")
		}
	}
}

func main() {
	// 数据库路径（支持扩展名：.ddb, .duckdb, .db）
	// 也可以使用绝对路径，如："/path/to/duck.db"
	dbPath := "./testdata/gorm_example.db"

	// 使用 github.com/alifiroozi80/duckdb 驱动打开数据库连接
	db, err := gorm.Open(duckdb.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // 启用 SQL 日志
	})
	if err != nil {
		log.Fatalf("Failed to connect database: %v", err)
	}

	// 获取底层 sql.DB 以设置连接池参数
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to get database instance: %v", err)
	}
	defer sqlDB.Close()

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	fmt.Println("✅ 成功连接到 DuckDB 数据库")

	// 检查是否支持全文索引
	fmt.Println("\n🔍 检查全文索引支持...")
	checkFulltextIndexSupport(db)

	// 自动迁移（创建表）
	if err := db.AutoMigrate(&User{}, &Product{}); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	fmt.Println("✅ 数据库表迁移完成")

	// 清空表数据（用于示例演示，确保每次运行都是干净状态）
	if err := db.Exec("DELETE FROM users").Error; err != nil {
		log.Printf("Warning: Failed to clear users table: %v", err)
	}
	if err := db.Exec("DELETE FROM products").Error; err != nil {
		log.Printf("Warning: Failed to clear products table: %v", err)
	}

	// 并发写入示例：两个 goroutine 同时写入不同的表，每个使用独立连接
	fmt.Println("\n🚀 开始并发写入测试（每个请求使用独立连接）...")
	var wg sync.WaitGroup
	var readWg sync.WaitGroup
	userWriteDone := make(chan bool, 1) // 用于通知 User 表写入完成

	wg.Add(2)

	// Goroutine 1: 并发写入 User 表（使用独立连接）
	go func() {
		defer wg.Done()

		// 为这个 goroutine 创建独立的数据库连接
		dbConn, err := createDBConnection(dbPath)
		if err != nil {
			log.Printf("❌ [Goroutine 1] 创建数据库连接失败: %v", err)
			return
		}
		defer func() {
			if sqlDB, err := dbConn.DB(); err == nil {
				sqlDB.Close()
				fmt.Println("🔌 [Goroutine 1] 数据库连接已关闭")
			}
		}()

		fmt.Println("🔌 [Goroutine 1] 已建立新的数据库连接")
		fmt.Println("📝 [Goroutine 1] 开始写入 User 表...")

		for i := 1; i <= 10; i++ {
			user := User{
				Name:  fmt.Sprintf("用户%d", i),
				Email: fmt.Sprintf("user%d@example.com", i),
				Age:   20 + i,
			}

			if err := dbConn.Create(&user).Error; err != nil {
				log.Printf("❌ [Goroutine 1] 创建用户失败: %v", err)
				return
			}
			fmt.Printf("✅ [Goroutine 1] 创建用户成功: ID=%d, Name=%s\n", user.ID, user.Name)
			time.Sleep(50 * time.Millisecond) // 模拟写入间隔
		}

		fmt.Println("✅ [Goroutine 1] User 表写入完成")
		// 通知 User 表写入完成
		userWriteDone <- true
	}()

	// Goroutine 2: 并发写入 Product 表（使用独立连接）
	go func() {
		defer wg.Done()

		// 为这个 goroutine 创建独立的数据库连接
		dbConn, err := createDBConnection(dbPath)
		if err != nil {
			log.Printf("❌ [Goroutine 2] 创建数据库连接失败: %v", err)
			return
		}
		defer func() {
			if sqlDB, err := dbConn.DB(); err == nil {
				sqlDB.Close()
				fmt.Println("🔌 [Goroutine 2] 数据库连接已关闭")
			}
		}()

		fmt.Println("🔌 [Goroutine 2] 已建立新的数据库连接")
		fmt.Println("📦 [Goroutine 2] 开始写入 Product 表...")

		for i := 1; i <= 10; i++ {
			product := Product{
				Name:  fmt.Sprintf("产品%d", i),
				Price: float64(i) * 10.5,
				Stock: 100 - i,
			}

			if err := dbConn.Create(&product).Error; err != nil {
				log.Printf("❌ [Goroutine 2] 创建产品失败: %v", err)
				return
			}
			fmt.Printf("✅ [Goroutine 2] 创建产品成功: ID=%d, Name=%s, Price=%.2f\n",
				product.ID, product.Name, product.Price)
			time.Sleep(50 * time.Millisecond) // 模拟写入间隔
		}

		fmt.Println("✅ [Goroutine 2] Product 表写入完成")
	}()

	// 等待 User 表写入完成，然后启动读取线程（使用独立连接）
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		<-userWriteDone // 等待 User 表写入完成

		// 为读取线程创建独立的数据库连接
		dbConn, err := createDBConnection(dbPath)
		if err != nil {
			log.Printf("❌ [读取线程] 创建数据库连接失败: %v", err)
			return
		}
		defer func() {
			if sqlDB, err := dbConn.DB(); err == nil {
				sqlDB.Close()
				fmt.Println("🔌 [读取线程] 数据库连接已关闭")
			}
		}()

		fmt.Println("🔌 [读取线程] 已建立新的数据库连接")
		fmt.Println("\n📖 [读取线程] 开始读取 User 表...")

		// 查询所有用户
		var allUsers []User
		if err := dbConn.Find(&allUsers).Error; err != nil {
			log.Printf("❌ [读取线程] 查询用户失败: %v", err)
			return
		}

		fmt.Printf("✅ [读取线程] 成功读取 %d 个用户:\n", len(allUsers))
		for _, u := range allUsers {
			fmt.Printf("  📋 [读取线程] ID=%d, Name=%s, Email=%s, Age=%d\n",
				u.ID, u.Name, u.Email, u.Age)
		}

		// 统计用户数量
		var userCount int64
		dbConn.Model(&User{}).Count(&userCount)
		fmt.Printf("✅ [读取线程] User 表共有 %d 条记录\n", userCount)

		fmt.Println("✅ [读取线程] User 表读取完成")
	}()

	// 等待两个写入 goroutine 完成
	wg.Wait()
	fmt.Println("\n🎉 并发写入测试完成！")

	// 等待读取线程完成
	readWg.Wait()

	// 示例：查询单个用户
	fmt.Println("\n🔍 查询单个用户...")
	var foundUser User
	if err := db.First(&foundUser).Error; err != nil {
		log.Printf("Warning: Failed to find user: %v", err)
	} else {
		fmt.Printf("✅ 找到用户: ID=%d, Name=%s, Email=%s, Age=%d\n",
			foundUser.ID, foundUser.Name, foundUser.Email, foundUser.Age)
	}

	// 示例：查询所有用户
	fmt.Println("\n🔍 查询所有用户...")
	var allUsers []User
	if err := db.Find(&allUsers).Error; err != nil {
		log.Fatalf("Failed to find users: %v", err)
	}
	fmt.Printf("✅ 找到 %d 个用户:\n", len(allUsers))
	for _, u := range allUsers {
		fmt.Printf("  - ID=%d, Name=%s, Email=%s, Age=%d\n", u.ID, u.Name, u.Email, u.Age)
	}

	// 示例：查询所有产品
	fmt.Println("\n🔍 查询所有产品...")
	var allProducts []Product
	if err := db.Find(&allProducts).Error; err != nil {
		log.Fatalf("Failed to find products: %v", err)
	}
	fmt.Printf("✅ 找到 %d 个产品:\n", len(allProducts))
	for _, p := range allProducts {
		fmt.Printf("  - ID=%d, Name=%s, Price=%.2f, Stock=%d\n", p.ID, p.Name, p.Price, p.Stock)
	}

	// 示例：条件查询
	fmt.Println("\n🔍 查询年龄大于 25 的用户...")
	var olderUsers []User
	if err := db.Where("age > ?", 25).Find(&olderUsers).Error; err != nil {
		log.Printf("Warning: Failed to find users: %v", err)
	} else {
		fmt.Printf("✅ 找到 %d 个用户:\n", len(olderUsers))
		for _, u := range olderUsers {
			fmt.Printf("  - ID=%d, Name=%s, Age=%d\n", u.ID, u.Name, u.Age)
		}
	}

	// 示例：统计用户数量
	fmt.Println("\n📊 统计用户数量...")
	var userCount int64
	db.Model(&User{}).Count(&userCount)
	fmt.Printf("✅ 当前共有 %d 个用户\n", userCount)

	// 示例：统计产品数量
	fmt.Println("\n📊 统计产品数量...")
	var productCount int64
	db.Model(&Product{}).Count(&productCount)
	fmt.Printf("✅ 当前共有 %d 个产品\n", productCount)

	fmt.Println("\n🎉 所有操作完成！")
}
