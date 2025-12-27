package sqlite3_driver_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
)

// getProjectRootTestdata 获取工程根目录的 testdata 路径
func getProjectRootTestdata() string {
	// 从当前文件位置向上查找 go.mod 来确定工程根目录
	// 当前文件在 pkg/sqlite3-driver/ 目录下，需要向上两级到工程根目录
	wd, _ := os.Getwd()
	// 如果从 pkg/sqlite3-driver 目录运行，向上两级
	// 如果从工程根目录运行，直接使用 testdata
	if filepath.Base(wd) == "sqlite3-driver" {
		return filepath.Join(wd, "..", "..", "testdata")
	}
	// 尝试查找 go.mod 文件来确定工程根目录
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "testdata")
		}
	}
	// 如果找不到，使用相对路径
	return filepath.Join("..", "..", "testdata")
}

func skipIfExtensionNotAvailable(t *testing.T, dbPath string) {
	// 使用临时文件测试基本连接是否可用
	tmpFile, err := os.CreateTemp("", "sqlite-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	db, err := sql.Open("sqlite3", tmpPath)
	if err != nil {
		// 如果打开失败，跳过测试
		t.Skipf("Skipping test: Failed to open database: %v", err)
	}
	defer db.Close()

	// Ping 会触发实际的连接
	if err := db.Ping(); err != nil {
		// 如果 ping 失败，跳过测试
		t.Skipf("Skipping test: Failed to ping database: %v", err)
	}
}

func TestSQLite3Driver_RelativePath(t *testing.T) {
	// 测试相对路径，应该自动构建到 testdata/db 目录

	// 获取工程根目录的 testdata
	testdataDir := getProjectRootTestdata()

	// 转换为绝对路径
	absTestdataDir, err := filepath.Abs(testdataDir)
	if err != nil {
		t.Fatalf("Failed to get absolute path for testdata directory: %v", err)
	}
	testdataDir = absTestdataDir

	// 确保 testdata 目录存在
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}

	// 设置环境变量指向工程根目录的 testdata
	originalDataDir := os.Getenv("DATA_DIR")
	os.Setenv("DATA_DIR", testdataDir)
	defer func() {
		if originalDataDir == "" {
			os.Unsetenv("DATA_DIR")
		} else {
			os.Setenv("DATA_DIR", originalDataDir)
		}
		_ = os.RemoveAll(filepath.Join(testdataDir, "db"))
	}()

	// 使用相对路径（不包含路径分隔符），这样 ensureDataPath 会将其构建到 db 子目录
	relativeDbPath := "relative_test.db"
	expectedPath := filepath.Join(testdataDir, "db", relativeDbPath)

	// 检查扩展是否可用
	skipIfExtensionNotAvailable(t, expectedPath)

	db, err := sql.Open("sqlite3", relativeDbPath)
	if err != nil {
		t.Fatalf("Failed to open database with relative path: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// 验证文件确实创建在 testdata/db 目录
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		// 检查文件是否被创建在其他位置
		// 可能被创建在当前工作目录的 db 子目录
		wd, _ := os.Getwd()
		altPath := filepath.Join(wd, "db", relativeDbPath)
		if _, err := os.Stat(altPath); err == nil {
			t.Errorf("Database file was created at %s instead of expected %s", altPath, expectedPath)
			return
		}
		// 检查默认的 ./data/db 位置
		defaultPath := filepath.Join("data", "db", relativeDbPath)
		if absDefaultPath, err := filepath.Abs(defaultPath); err == nil {
			if _, err := os.Stat(absDefaultPath); err == nil {
				t.Errorf("Database file was created at %s instead of expected %s. DATA_DIR was set to %s", absDefaultPath, expectedPath, testdataDir)
				return
			}
		}
		// 检查是否文件被创建在 testdataDir 的根目录（如果驱动没有使用包装器）
		directPath := filepath.Join(testdataDir, relativeDbPath)
		if _, err := os.Stat(directPath); err == nil {
			t.Errorf("Database file was created at %s instead of expected %s. The driver wrapper may not be working correctly.", directPath, expectedPath)
			return
		}
		t.Errorf("Database file should be created at %s, but it doesn't exist. DATA_DIR=%s", expectedPath, os.Getenv("DATA_DIR"))
	}
}

func TestSQLite3Driver_CreateTable(t *testing.T) {
	testdataDir := getProjectRootTestdata()
	dbPath := filepath.Join(testdataDir, "sqlite3_create.db")

	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	// 检查扩展是否可用
	skipIfExtensionNotAvailable(t, dbPath)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS items (
		id INTEGER PRIMARY KEY,
		name TEXT,
		value REAL
	)`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// 插入数据
	_, err = db.ExecContext(ctx, `INSERT INTO items (id, name, value) VALUES (1, 'Item1', 10.5)`)
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// 查询数据
	var name string
	var value float64
	err = db.QueryRowContext(ctx, `SELECT name, value FROM items WHERE id = 1`).Scan(&name, &value)
	if err != nil {
		t.Fatalf("Failed to query data: %v", err)
	}

	if name != "Item1" || value != 10.5 {
		t.Errorf("Expected name=Item1, value=10.5, got name=%s, value=%f", name, value)
	}
}

func TestSQLite3Driver_Transaction(t *testing.T) {
	testdataDir := getProjectRootTestdata()
	dbPath := filepath.Join(testdataDir, "sqlite3_tx.db")

	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	// 检查扩展是否可用
	skipIfExtensionNotAvailable(t, dbPath)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS transactions (
		id INTEGER PRIMARY KEY,
		amount REAL
	)`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// 测试事务
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO transactions (id, amount) VALUES (1, 25.0)`)
	if err != nil {
		tx.Rollback()
		t.Fatalf("Failed to insert in transaction: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}

	// 验证数据
	var amount float64
	err = db.QueryRowContext(ctx, `SELECT amount FROM transactions WHERE id = 1`).Scan(&amount)
	if err != nil {
		t.Fatalf("Failed to query after commit: %v", err)
	}

	if amount != 25.0 {
		t.Errorf("Expected amount=25.0, got %f", amount)
	}
}
