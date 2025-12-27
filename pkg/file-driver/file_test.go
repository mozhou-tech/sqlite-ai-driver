package file_driver

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// getProjectRootTestdata 获取工程根目录的 testdata 路径
func getProjectRootTestdata() string {
	// 从当前文件位置向上查找 go.mod 来确定工程根目录
	wd, _ := os.Getwd()
	// 如果从 pkg/file-driver 目录运行，向上两级
	// 如果从工程根目录运行，直接使用 testdata
	if filepath.Base(wd) == "file-driver" {
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

func TestFileDriver_OpenLocalFile(t *testing.T) {
	// 使用工程根目录的 testdata
	testdataDir := getProjectRootTestdata()
	dbPath := filepath.Join(testdataDir, "file_test.db")

	// 确保 testdata 目录存在
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}

	// 清理测试文件
	defer func() {
		_ = os.Remove(dbPath)
	}()

	db, err := sql.Open("file", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// 测试连接
	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}
}

func TestFileDriver_OpenWithFileScheme(t *testing.T) {
	testdataDir := getProjectRootTestdata()
	dbPath := filepath.Join(testdataDir, "file_scheme.db")

	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	// 测试使用 file:// 协议
	db, err := sql.Open("file", "file://"+dbPath)
	if err != nil {
		t.Fatalf("Failed to open database with file:// scheme: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}
}

func TestFileDriver_RelativePath(t *testing.T) {
	// 测试相对路径，应该自动构建到 testdata/files 目录
	dbPath := "relative_file_test.db"

	// 获取工程根目录的 testdata
	testdataDir := getProjectRootTestdata()
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
		_ = os.RemoveAll(filepath.Join(testdataDir, "files"))
	}()

	db, err := sql.Open("file", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database with relative path: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// 验证文件确实创建在 testdata/files 目录
	expectedPath := filepath.Join(testdataDir, "files", dbPath)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Database file should be created at %s", expectedPath)
	}
}

func TestFileDriver_CreateTable(t *testing.T) {
	testdataDir := getProjectRootTestdata()
	dbPath := filepath.Join(testdataDir, "file_create.db")

	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	db, err := sql.Open("file", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS products (
		id INTEGER PRIMARY KEY,
		name TEXT,
		price REAL
	)`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// 插入数据
	_, err = db.ExecContext(ctx, `INSERT INTO products (id, name, price) VALUES (1, 'Laptop', 999.99)`)
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// 查询数据
	var name string
	var price float64
	err = db.QueryRowContext(ctx, `SELECT name, price FROM products WHERE id = 1`).Scan(&name, &price)
	if err != nil {
		t.Fatalf("Failed to query data: %v", err)
	}

	if name != "Laptop" || price != 999.99 {
		t.Errorf("Expected name=Laptop, price=999.99, got name=%s, price=%f", name, price)
	}
}

func TestFileDriver_Transaction(t *testing.T) {
	testdataDir := getProjectRootTestdata()
	dbPath := filepath.Join(testdataDir, "file_tx.db")

	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	db, err := sql.Open("file", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS orders (
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

	_, err = tx.ExecContext(ctx, `INSERT INTO orders (id, amount) VALUES (1, 50.0)`)
	if err != nil {
		tx.Rollback()
		t.Fatalf("Failed to insert in transaction: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}

	// 验证数据
	var amount float64
	err = db.QueryRowContext(ctx, `SELECT amount FROM orders WHERE id = 1`).Scan(&amount)
	if err != nil {
		t.Fatalf("Failed to query after commit: %v", err)
	}

	if amount != 50.0 {
		t.Errorf("Expected amount=50.0, got %f", amount)
	}
}

func TestFileDriver_EmptyPath(t *testing.T) {
	// 测试空路径应该返回错误
	db, err := sql.Open("file", "")
	if err != nil {
		// sql.Open 可能不会立即返回错误，需要 ping 来触发
		return
	}
	defer db.Close()

	// ping 应该触发 Open 方法并返回错误
	if err := db.Ping(); err == nil {
		t.Error("Expected error for empty path, got nil")
	}
}

func TestFileDriver_AbsolutePath(t *testing.T) {
	// 测试绝对路径
	testdataDir := getProjectRootTestdata()
	absPath, err := filepath.Abs(filepath.Join(testdataDir, "file_abs.db"))
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(absPath)
	}()

	db, err := sql.Open("file", absPath)
	if err != nil {
		t.Fatalf("Failed to open database with absolute path: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}
}
