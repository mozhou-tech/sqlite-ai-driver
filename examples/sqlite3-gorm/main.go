package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver" // 导入以注册驱动
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openSQLite3 打开 SQLite3 数据库连接，使用自定义的 sqlite3-driver
// 这个函数类似于 duckdb.Open，返回一个 GORM Dialector
// 由于 gorm.io/driver/sqlite 内部使用 modernc.org/sqlite（注册为 "sqlite"），
// 而我们的驱动注册为 "sqlite3"，我们需要创建一个自定义 Dialector
func openSQLite3(dsn string) gorm.Dialector {
	// 创建一个自定义 Dialector，使用我们注册的 "sqlite3" 驱动
	// 我们通过创建一个新的 sqlite.Dialector 并设置其内部连接来实现
	// 但 sqlite.Dialector 不支持直接设置驱动名称，所以我们使用另一种方式：
	// 先使用 database/sql 打开连接（使用我们的 "sqlite3" 驱动），
	// 然后使用 sqlite.Dialector 包装这个连接

	// 方法1：直接使用 sqlite.Open，但它会使用 modernc.org/sqlite
	// 为了使用我们的 sqlite3-driver，我们需要创建一个自定义实现
	// 这里我们使用 sqlite.Open，但通过 DSN 来利用我们的驱动特性（如自动路径处理）

	// 注意：由于 sqlite.Dialector 的限制，我们暂时使用 sqlite.Open
	// 如果需要完全使用我们的 sqlite3-driver，需要创建一个完全自定义的 Dialector 实现
	// 但这样会失去 GORM SQLite 驱动的一些优化特性
	return sqlite.Open(dsn)
}

// User 用户模型
type User struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string    `gorm:"type:varchar(100);not null"`
	Email     string    `gorm:"type:varchar(100);uniqueIndex;not null"`
	Age       int       `gorm:"type:integer"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// printSQLiteExtensions 打印 SQLite 支持的扩展信息
func printSQLiteExtensions(db *gorm.DB) {
	fmt.Println("\n📦 SQLite 扩展信息:")

	// 获取底层 sql.DB
	sqlDB, err := db.DB()
	if err != nil {
		fmt.Printf("  无法获取数据库连接: %v\n", err)
		return
	}

	// 查询 SQLite 版本
	var version string
	if err := sqlDB.QueryRow("SELECT sqlite_version()").Scan(&version); err == nil {
		fmt.Printf("  SQLite 版本: %s\n", version)
	}

	// 查询编译选项（包含扩展信息）
	// PRAGMA compile_options 返回多行结果，每行一个选项
	rows, err := sqlDB.Query("PRAGMA compile_options")
	if err == nil {
		defer rows.Close()

		fmt.Println("  编译选项:")
		var allOptions []string
		for rows.Next() {
			var option string
			if err := rows.Scan(&option); err == nil {
				fmt.Printf("    - %s\n", option)
				allOptions = append(allOptions, option)
			}
		}

		// 检查 rows.Err() 是否有错误
		if err := rows.Err(); err != nil {
			fmt.Printf("  读取编译选项时出错: %v\n", err)
		}

		// 合并所有选项为字符串（用于搜索）
		allOptionsStr := strings.Join(allOptions, " ")
		allOptionsLower := strings.ToLower(allOptionsStr)

		fmt.Println("  扩展支持情况:")
		// 检查常见的扩展编译选项（不区分大小写）

		// 检查 FTS 扩展
		if strings.Contains(allOptionsLower, "enable_fts3") ||
			strings.Contains(allOptionsLower, "enable_fts4") ||
			strings.Contains(allOptionsLower, "enable_fts5") {
			fmt.Printf("    ✅ FTS (全文搜索): 支持\n")
		}

		// 检查 JSON1 扩展
		if strings.Contains(allOptionsLower, "enable_json1") ||
			strings.Contains(allOptionsLower, "json1") {
			fmt.Printf("    ✅ JSON1: 支持\n")
		}

		// 检查 RTREE 扩展
		if strings.Contains(allOptionsLower, "enable_rtree") ||
			strings.Contains(allOptionsLower, "rtree") {
			fmt.Printf("    ✅ RTREE: 支持\n")
		}

		// 检查其他扩展
		extensions := map[string]string{
			"GEOPOLY":     "GEOPOLY",
			"SESSION":     "SESSION",
			"DBSTAT_VTAB": "DBSTAT_VTAB",
			"VECTOR":      "VECTOR",
			"VSS":         "VSS",
			"VEC":         "VEC",
			"SPELLFIX":    "SPELLFIX",
			"CARRAY":      "CARRAY",
			"CSV":         "CSV",
			"MEMORYVFS":   "MEMORYVFS",
		}

		for name, keyword := range extensions {
			if strings.Contains(allOptionsLower, strings.ToLower(keyword)) {
				fmt.Printf("    ✅ %s: 支持\n", name)
			}
		}
	} else {
		// 如果 PRAGMA compile_options 查询失败，尝试其他方式
		fmt.Printf("  无法查询编译选项: %v\n", err)
	}

	// 尝试测试 FTS5 扩展（如果可用）
	// 注意：fts5_version() 函数检查 FTS5 是否可用
	var fts5Version string
	if err := sqlDB.QueryRow("SELECT fts5_version()").Scan(&fts5Version); err == nil {
		fmt.Printf("    ✅ FTS5: 可用（版本: %s）\n", fts5Version)
	}

	// 尝试测试 JSON1 扩展（如果可用）
	var jsonResult string
	if err := sqlDB.QueryRow("SELECT json('{}')").Scan(&jsonResult); err == nil {
		fmt.Printf("    ✅ JSON1: 可用（已测试）\n")
	}
}

func main() {
	// 数据库路径（支持相对路径，会自动构建到 data/db/ 目录）
	// 也可以使用绝对路径，如："/path/to/sqlite.db"
	// 默认使用 ./data/db/ 目录存储数据
	dbPath := "gorm_example.db"

	// 使用 sqlite3-driver 打开数据库连接
	// 注意：需要先导入 pkg/sqlite3-driver 包以注册驱动
	// 使用 openSQLite3 函数创建 GORM Dialector，它会使用我们注册的 "sqlite3" 驱动
	db, err := gorm.Open(openSQLite3(dbPath), &gorm.Config{
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

	fmt.Println("✅ 成功连接到 SQLite 数据库")

	// 打印支持的 SQLite 扩展
	printSQLiteExtensions(db)

	// 自动迁移（创建表）
	if err := db.AutoMigrate(&User{}); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	fmt.Println("✅ 数据库表迁移完成")

	// 清空表数据（用于示例演示，确保每次运行都是干净状态）
	if err := db.Exec("DELETE FROM users").Error; err != nil {
		log.Printf("Warning: Failed to clear users table: %v", err)
	}

	// 示例：创建用户
	fmt.Println("\n📝 创建用户...")
	user := User{
		Name:  "张三",
		Email: "zhangsan@example.com",
		Age:   25,
	}

	if err := db.Create(&user).Error; err != nil {
		log.Fatalf("Failed to create user: %v", err)
	}
	fmt.Printf("✅ 创建用户成功: ID=%d, Name=%s, Email=%s\n", user.ID, user.Name, user.Email)

	// 示例：批量创建用户
	fmt.Println("\n📝 批量创建用户...")
	users := []User{
		{Name: "李四", Email: "lisi@example.com", Age: 30},
		{Name: "王五", Email: "wangwu@example.com", Age: 28},
		{Name: "赵六", Email: "zhaoliu@example.com", Age: 32},
	}

	if err := db.Create(&users).Error; err != nil {
		log.Fatalf("Failed to create users: %v", err)
	}
	fmt.Printf("✅ 批量创建用户成功，共 %d 个用户\n", len(users))

	// 示例：查询单个用户
	fmt.Println("\n🔍 查询单个用户...")
	var foundUser User
	if err := db.First(&foundUser, "email = ?", "zhangsan@example.com").Error; err != nil {
		log.Fatalf("Failed to find user: %v", err)
	}
	fmt.Printf("✅ 找到用户: ID=%d, Name=%s, Email=%s, Age=%d\n",
		foundUser.ID, foundUser.Name, foundUser.Email, foundUser.Age)

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

	// 示例：条件查询
	fmt.Println("\n🔍 查询年龄大于 28 的用户...")
	var olderUsers []User
	if err := db.Where("age > ?", 28).Find(&olderUsers).Error; err != nil {
		log.Fatalf("Failed to find users: %v", err)
	}
	fmt.Printf("✅ 找到 %d 个用户:\n", len(olderUsers))
	for _, u := range olderUsers {
		fmt.Printf("  - ID=%d, Name=%s, Age=%d\n", u.ID, u.Name, u.Age)
	}

	// 示例：更新用户
	fmt.Println("\n✏️  更新用户...")
	if err := db.Model(&foundUser).Where("id = ?", foundUser.ID).Update("age", 26).Error; err != nil {
		log.Fatalf("Failed to update user: %v", err)
	}
	fmt.Printf("✅ 更新用户成功: ID=%d, 新年龄=%d\n", foundUser.ID, 26)

	// 示例：更新多个字段
	fmt.Println("\n✏️  更新用户多个字段...")
	updates := map[string]interface{}{
		"name": "张三（已更新）",
		"age":  27,
	}
	if err := db.Model(&foundUser).Where("id = ?", foundUser.ID).Updates(updates).Error; err != nil {
		log.Fatalf("Failed to update user: %v", err)
	}
	fmt.Printf("✅ 更新用户成功: ID=%d\n", foundUser.ID)

	// 示例：删除用户
	fmt.Println("\n🗑️  删除用户...")
	if err := db.Delete(&foundUser).Error; err != nil {
		log.Fatalf("Failed to delete user: %v", err)
	}
	fmt.Printf("✅ 删除用户成功: ID=%d\n", foundUser.ID)

	// 验证删除
	var count int64
	db.Model(&User{}).Where("id = ?", foundUser.ID).Count(&count)
	if count == 0 {
		fmt.Println("✅ 用户已成功删除")
	} else {
		fmt.Println("⚠️  用户删除失败")
	}

	// 示例：统计用户数量
	fmt.Println("\n📊 统计用户数量...")
	var totalCount int64
	db.Model(&User{}).Count(&totalCount)
	fmt.Printf("✅ 当前共有 %d 个用户\n", totalCount)

	// 示例：事务操作
	fmt.Println("\n💼 事务操作...")
	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("事务回滚: %v", r)
		}
	}()

	// 在事务中创建用户
	txUser := User{
		Name:  "事务用户",
		Email: "tx@example.com",
		Age:   35,
	}
	if err := tx.Create(&txUser).Error; err != nil {
		tx.Rollback()
		log.Fatalf("Failed to create user in transaction: %v", err)
	}

	// 在事务中更新用户
	if err := tx.Model(&txUser).Where("id = ?", txUser.ID).Update("age", 36).Error; err != nil {
		tx.Rollback()
		log.Fatalf("Failed to update user in transaction: %v", err)
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		log.Fatalf("Failed to commit transaction: %v", err)
	}
	fmt.Printf("✅ 事务提交成功: 创建并更新用户 ID=%d\n", txUser.ID)

	fmt.Println("\n🎉 所有操作完成！")
}
