package sqlite3_driver

import (
	"os"
	"testing"

	"gorm.io/gorm"
)

func TestDialector(t *testing.T) {
	// 测试 Dialector 的基本功能
	dialector := Open("test_dialector.db")

	if dialector.Name() != "sqlite3" {
		t.Errorf("Expected dialector name to be 'sqlite3', got '%s'", dialector.Name())
	}

	// 使用 gorm.Open 来测试，这会正确初始化 DB 并调用 Initialize
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 验证连接是否正常
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("Failed to get sql.DB: %v", err)
	}

	// 测试 Ping
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// 清理测试数据库
	sqlDB.Close()
	os.Remove("test_dialector.db")
	os.Remove("data/db/test_dialector.db")
}
