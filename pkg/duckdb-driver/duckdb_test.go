package duckdb_driver

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestDuckDBDriver_Open(t *testing.T) {
	// 使用 testdata 目录
	dbPath := filepath.Join("testdata", "duckdb_test.db")

	// 确保 testdata 目录存在
	if err := os.MkdirAll("testdata", 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}

	// 清理测试文件
	defer func() {
		_ = os.Remove(dbPath)
	}()

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// 测试连接
	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}
}

func TestDuckDBDriver_RelativePath(t *testing.T) {
	// 测试相对路径，应该自动构建到 testdata/duck 目录
	dbPath := "relative_test.db"

	// 确保 testdata 目录存在
	if err := os.MkdirAll("testdata", 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}

	// 设置环境变量指向 testdata
	originalDataDir := os.Getenv("DATA_DIR")
	os.Setenv("DATA_DIR", "testdata")
	defer func() {
		if originalDataDir == "" {
			os.Unsetenv("DATA_DIR")
		} else {
			os.Setenv("DATA_DIR", originalDataDir)
		}
		_ = os.RemoveAll(filepath.Join("testdata", "duck"))
	}()

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database with relative path: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// 执行一个写入操作来触发文件创建（DuckDB 只在有写入操作时才创建文件）
	ctx := context.Background()
	_, err = db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS test_table (id INTEGER)")
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	// 验证文件确实创建在 testdata/duck 目录
	expectedPath := filepath.Join("testdata", "duck", dbPath)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Database file should be created at %s", expectedPath)
	}
}

func TestDuckDBDriver_CreateTable(t *testing.T) {
	dbPath := filepath.Join("testdata", "duckdb_create.db")

	if err := os.MkdirAll("testdata", 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		name VARCHAR(100),
		email VARCHAR(100)
	)`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// 插入数据
	_, err = db.ExecContext(ctx, `INSERT INTO users (id, name, email) VALUES (1, 'Alice', 'alice@example.com')`)
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// 查询数据
	var name, email string
	err = db.QueryRowContext(ctx, `SELECT name, email FROM users WHERE id = 1`).Scan(&name, &email)
	if err != nil {
		t.Fatalf("Failed to query data: %v", err)
	}

	if name != "Alice" || email != "alice@example.com" {
		t.Errorf("Expected name=Alice, email=alice@example.com, got name=%s, email=%s", name, email)
	}
}

func TestDuckDBDriver_Transaction(t *testing.T) {
	dbPath := filepath.Join("testdata", "duckdb_tx.db")

	if err := os.MkdirAll("testdata", 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS accounts (
		id INTEGER PRIMARY KEY,
		balance INTEGER
	)`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// 测试事务
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO accounts (id, balance) VALUES (1, 100)`)
	if err != nil {
		tx.Rollback()
		t.Fatalf("Failed to insert in transaction: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}

	// 验证数据
	var balance int
	err = db.QueryRowContext(ctx, `SELECT balance FROM accounts WHERE id = 1`).Scan(&balance)
	if err != nil {
		t.Fatalf("Failed to query after commit: %v", err)
	}

	if balance != 100 {
		t.Errorf("Expected balance=100, got %d", balance)
	}
}

func TestDuckDBDriver_Extensions(t *testing.T) {
	dbPath := filepath.Join("testdata", "duckdb_ext.db")

	if err := os.MkdirAll("testdata", 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 测试扩展是否已加载（尝试使用 sqlite 扩展读取 SQLite 文件）
	// 注意：这里只是测试扩展加载不会报错，实际功能测试可能需要真实的 SQLite 文件
	_, err = db.ExecContext(ctx, `SELECT 1`)
	if err != nil {
		t.Fatalf("Failed to execute query after extension load: %v", err)
	}
}

func TestDuckDBDriver_QueryParams(t *testing.T) {
	dbPath := filepath.Join("testdata", "duckdb_params.db")

	if err := os.MkdirAll("testdata", 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	// 测试带查询参数的 DSN
	dsn := dbPath + "?access_mode=read_write"
	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		t.Fatalf("Failed to open database with query params: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}
}
