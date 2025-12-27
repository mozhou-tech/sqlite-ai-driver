package main

import (
	"fmt"
	"log"
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
