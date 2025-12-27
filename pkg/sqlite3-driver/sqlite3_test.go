package sqlite3_driver_test

import (
	"bufio"
	"context"
	"database/sql"
	"math"
	"os"
	"strings"
	"testing"

	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
)

func skipIfExtensionNotAvailable(t *testing.T, dbPath string) {
	// 使用临时文件测试扩展是否可用
	tmpFile, err := os.CreateTemp("", "sqlite-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	db, err := sql.Open("sqlite-vss", tmpPath)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "extension load error") || strings.Contains(errStr, "vss0") {
			t.Skipf("Skipping test: SQLite extensions not available: %v", err)
		}
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Ping 会触发实际的连接和扩展加载
	if err := db.Ping(); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "extension load error") || strings.Contains(errStr, "vss0") {
			t.Skipf("Skipping test: SQLite extensions not available: %v", err)
		}
		t.Fatalf("Failed to ping database: %v", err)
	}
}

func TestOpen(t *testing.T) {
	dbPath := "testdata/test.db"
	skipIfExtensionNotAvailable(t, dbPath)

	db, err := sql.Open("sqlite-vss", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Cleanup(func() {
		_ = os.Remove(dbPath)
	})

	r := db.QueryRow("select vss_version();")
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}

	var version string
	if err := r.Scan(&version); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(version, "v") {
		t.Errorf("version should be start with 'v', but got %s", version)
	}
}

// ref: https://github.com/koron/techdocs/blob/main/sqlite-vss-getting-started/doc.md
func TestVectorSearch(t *testing.T) {
	ctx := context.Background()
	dbPath := "testdata/vec.db"
	skipIfExtensionNotAvailable(t, dbPath)

	db, err := sql.Open("sqlite-vss", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Remove(dbPath)
	})

	// Create table
	if _, err := db.ExecContext(ctx, `CREATE TABLE words (
  label TEXT
);`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE VIRTUAL TABLE vss_words USING vss0(
  vector(300)
);`); err != nil {
		t.Fatal(err)
	}

	// read test.vec line by line.
	f, err := os.Open("testdata/test.vec")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = f.Close()
	})
	scanner := bufio.NewScanner(f)
	scanner.Split(bufio.ScanLines)

	// food
	var foodvec string

	rowid := 1
	for scanner.Scan() {
		row := scanner.Text()
		splitted := strings.Split(row, " ")
		label := splitted[0]
		vec := "[" + strings.Join(splitted[1:], ",") + "]"
		if label == "food" {
			foodvec = vec
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO words (rowid, label) VALUES (?, ?);`, rowid, label); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO vss_words(rowid, vector) VALUES (?, json(?));`, rowid, vec); err != nil {
			t.Fatal(err)
		}
		rowid++
	}

	if _, err := db.ExecContext(ctx, `VACUUM;`); err != nil {
		t.Fatal(err)
	}

	rows, err := db.QueryContext(ctx, `SELECT w.label, v.distance FROM vss_words AS v
  JOIN words AS w ON w.rowid = v.rowid
  WHERE vss_search(
    v.vector,
    vss_search_params(
      json(?),
      10
    )
  );`, foodvec)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	got := map[string]float64{}
	for rows.Next() {
		var (
			label string
			dist  float64
		)
		if err := rows.Scan(&label, &dist); err != nil {
			t.Fatal(err)
		}
		got[label] = dist
	}

	if want := 10; len(got) != want {
		t.Errorf("len(got) = %d, want %d", len(got), want)
	}

	if want := 0.0; got["food"] != want {
		t.Errorf("got[food] = %f, want %f", got["food"], want)
	}

	if want := 1.828758; math.Round(got["health"]*10000)/10000 != math.Round(want*10000)/10000 {
		t.Errorf("got[health] = %f, want %f", got["health"], want)
	}
}

func TestSQLite3Driver_RelativePath(t *testing.T) {
	// 测试相对路径，应该自动构建到 testdata/db 目录
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
		_ = os.RemoveAll("testdata/db")
	}()

	// 检查扩展是否可用
	skipIfExtensionNotAvailable(t, "testdata/db/"+dbPath)

	db, err := sql.Open("sqlite-vss", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database with relative path: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// 验证文件确实创建在 testdata/db 目录
	expectedPath := "testdata/db/" + dbPath
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Database file should be created at %s", expectedPath)
	}
}

func TestSQLite3Driver_CreateTable(t *testing.T) {
	dbPath := "testdata/sqlite3_create.db"

	if err := os.MkdirAll("testdata", 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	// 检查扩展是否可用
	skipIfExtensionNotAvailable(t, dbPath)

	db, err := sql.Open("sqlite-vss", dbPath)
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
	dbPath := "testdata/sqlite3_tx.db"

	if err := os.MkdirAll("testdata", 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		_ = os.Remove(dbPath)
	}()

	// 检查扩展是否可用
	skipIfExtensionNotAvailable(t, dbPath)

	db, err := sql.Open("sqlite-vss", dbPath)
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
