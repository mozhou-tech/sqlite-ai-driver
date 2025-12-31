package sqlite3_driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ensureDataPath 确保数据路径存在
// workingDir: 工作目录，如果提供则相对路径会构建到 {workingDir}/data.db
// path: 数据库文件路径
func ensureDataPath(workingDir, path string) (string, error) {
	// 如果路径包含路径分隔符（绝对路径或相对路径），直接使用
	if strings.Contains(path, string(filepath.Separator)) || strings.Contains(path, "/") || strings.Contains(path, "\\") {
		// 转换为绝对路径（如果是相对路径）
		absPath, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("failed to get absolute path: %w", err)
		}

		// 确保目录存在
		dir := filepath.Dir(absPath)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", fmt.Errorf("failed to create directory: %w", err)
			}
		}
		return absPath, nil
	}

	// 如果是相对路径（不包含路径分隔符）
	if workingDir != "" {
		// 如果提供了 workingDir，构建到 {workingDir}/data.db
		// 将 workingDir 转换为绝对路径
		absWorkingDir, err := filepath.Abs(workingDir)
		if err != nil {
			return "", fmt.Errorf("failed to get absolute path for workingDir: %w", err)
		}
		fullPath := filepath.Join(absWorkingDir, "data.db")

		// 确保目录存在
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}

		return fullPath, nil
	}

	// 如果没有提供 workingDir，保持原有行为：构建到 ./data/db/ 子目录
	dataDir := "./data"
	// 转换为绝对路径
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		// 如果转换失败，使用原始路径
		absDataDir = dataDir
	}
	dataDir = absDataDir
	fullPath := filepath.Join(dataDir, "db", path)

	// 转换为绝对路径
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	return absPath, nil
}

func init() {
	// 注册 sqlite3 驱动（包装后的驱动，支持自动路径处理）
	// 我们注册为 "sqlite3" 以保持向后兼容性
	// 在内部使用 modernc.org/sqlite 驱动（已注册为 "sqlite"）
	// 使用 recover 捕获重复注册的 panic（可能其他包也注册了 "sqlite3"）
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 如果是因为重复注册而 panic，忽略它
				errStr := fmt.Sprintf("%v", r)
				if strings.Contains(errStr, "Register called twice for driver sqlite3") {
					return
				}
				// 其他 panic 重新抛出
				panic(r)
			}
		}()
		sql.Register("sqlite3", &sqliteDriverWrapper{})
	}()
}

// sqliteDriverWrapper 包装 SQLite 驱动，自动处理路径和 PRAGMA 设置
type sqliteDriverWrapper struct{}

func (w *sqliteDriverWrapper) Open(name string) (driver.Conn, error) {
	log.Printf("[sqlite3-driver] Open called with name: %s", name)

	// 解析连接字符串，提取 workingDir 参数和其他参数
	var workingDir string
	var dbPath string
	var queryParams url.Values

	// 检查是否包含查询参数
	if idx := strings.Index(name, "?"); idx != -1 {
		dbPath = name[:idx]
		queryStr := name[idx+1:]

		// 解析查询参数
		queryParams, _ = url.ParseQuery(queryStr)
		if wd := queryParams.Get("workingDir"); wd != "" {
			workingDir = wd
			log.Printf("[sqlite3-driver] Found workingDir parameter: %s", workingDir)
			// 从查询参数中移除 workingDir，因为它不是 SQLite 的参数
			queryParams.Del("workingDir")
		}
	} else {
		dbPath = name
		queryParams = make(url.Values)
	}

	log.Printf("[sqlite3-driver] Parsed dbPath: %s, workingDir: %s", dbPath, workingDir)

	// 自动构建路径并创建目录
	finalPath, err := ensureDataPath(workingDir, dbPath)
	if err != nil {
		log.Printf("[sqlite3-driver] ERROR: failed to ensure data path: %v", err)
		return nil, fmt.Errorf("failed to ensure data path: %w", err)
	}

	log.Printf("[sqlite3-driver] Final database path: %s", finalPath)

	// 构建 DSN，保留原有的查询参数（如 _pragma）
	// 如果没有 _pragma 参数，默认添加 journal_mode(WAL)
	if queryParams.Get("_pragma") == "" {
		queryParams.Set("_pragma", "journal_mode(WAL)")
		log.Printf("[sqlite3-driver] Added default _pragma: journal_mode(WAL)")
	}

	dsn := finalPath
	if len(queryParams) > 0 {
		dsn += "?" + queryParams.Encode()
	}

	log.Printf("[sqlite3-driver] Opening database with DSN: %s", dsn)

	// 使用已注册的 "sqlite" 驱动打开连接
	// 注意：由于 Go 的 database/sql 包设计，我们无法直接访问已注册的驱动实例
	// 因此我们使用 sql.Open 创建临时 DB，然后立即获取连接
	// 这样可以避免连接池管理问题
	startTime := time.Now()
	tempDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Printf("[sqlite3-driver] ERROR: failed to open database: %v (took %v)", err, time.Since(startTime))
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	log.Printf("[sqlite3-driver] Database opened successfully (took %v)", time.Since(startTime))

	// 设置连接池参数以避免死锁
	tempDB.SetMaxIdleConns(1)
	tempDB.SetMaxOpenConns(1)
	tempDB.SetConnMaxLifetime(0)
	log.Printf("[sqlite3-driver] Connection pool configured: MaxIdle=1, MaxOpen=1, MaxLifetime=0")

	// 先 Ping 测试连接，确保数据库可以正常访问
	pingStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = tempDB.PingContext(ctx)
	cancel()
	if err != nil {
		log.Printf("[sqlite3-driver] ERROR: failed to ping database: %v (took %v)", err, time.Since(pingStart))
		tempDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	log.Printf("[sqlite3-driver] Database ping successful (took %v)", time.Since(pingStart))

	// 获取底层连接，使用超时上下文避免死锁
	connStart := time.Now()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	conn, err := tempDB.Conn(ctx)
	cancel()
	if err != nil {
		log.Printf("[sqlite3-driver] ERROR: failed to get connection: %v (took %v)", err, time.Since(connStart))
		tempDB.Close()
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	log.Printf("[sqlite3-driver] Connection acquired successfully (took %v)", time.Since(connStart))

	// 返回包装的连接，在关闭时同时关闭 db 和 conn
	log.Printf("[sqlite3-driver] Open completed successfully, total time: %v", time.Since(startTime))
	return &sqliteConnWrapper{db: tempDB, conn: conn}, nil
}

// sqliteConnWrapper 包装 sql.DB 和 sql.Conn 以实现 driver.Conn 接口
type sqliteConnWrapper struct {
	db   *sql.DB
	conn *sql.Conn
}

func (c *sqliteConnWrapper) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.conn.PrepareContext(context.Background(), query)
	if err != nil {
		return nil, err
	}
	return &sqliteStmtWrapper{stmt: stmt}, nil
}

func (c *sqliteConnWrapper) Close() error {
	log.Printf("[sqlite3-driver] Closing connection")
	err1 := c.conn.Close()
	err2 := c.db.Close()
	if err1 != nil {
		log.Printf("[sqlite3-driver] ERROR: failed to close connection: %v", err1)
		return err1
	}
	if err2 != nil {
		log.Printf("[sqlite3-driver] ERROR: failed to close database: %v", err2)
		return err2
	}
	log.Printf("[sqlite3-driver] Connection closed successfully")
	return nil
}

func (c *sqliteConnWrapper) Begin() (driver.Tx, error) {
	tx, err := c.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	return &sqliteTxWrapper{tx: tx}, nil
}

// sqliteStmtWrapper 包装 sql.Stmt 以实现 driver.Stmt 接口
type sqliteStmtWrapper struct {
	stmt *sql.Stmt
}

func (s *sqliteStmtWrapper) Close() error {
	return s.stmt.Close()
}

func (s *sqliteStmtWrapper) NumInput() int {
	return -1 // 未知参数数量
}

func (s *sqliteStmtWrapper) Exec(args []driver.Value) (driver.Result, error) {
	return s.stmt.Exec(convertArgs(args)...)
}

func (s *sqliteStmtWrapper) Query(args []driver.Value) (driver.Rows, error) {
	rows, err := s.stmt.Query(convertArgs(args)...)
	if err != nil {
		return nil, err
	}
	return &sqliteRowsWrapper{rows: rows}, nil
}

// sqliteRowsWrapper 包装 sql.Rows 以实现 driver.Rows 接口
type sqliteRowsWrapper struct {
	rows *sql.Rows
}

func (r *sqliteRowsWrapper) Columns() []string {
	cols, err := r.rows.Columns()
	if err != nil {
		return nil
	}
	return cols
}

func (r *sqliteRowsWrapper) Close() error {
	return r.rows.Close()
}

func (r *sqliteRowsWrapper) Next(dest []driver.Value) error {
	if !r.rows.Next() {
		return r.rows.Err()
	}

	// 获取列数
	cols := r.Columns()
	if len(dest) < len(cols) {
		return fmt.Errorf("not enough destination values")
	}

	// 创建临时切片来接收值
	values := make([]interface{}, len(cols))
	for i := range values {
		values[i] = new(interface{})
	}

	// 扫描值
	if err := r.rows.Scan(values...); err != nil {
		return err
	}

	// 转换值
	for i, v := range values {
		if ptr, ok := v.(*interface{}); ok {
			dest[i] = *ptr
		} else {
			dest[i] = v
		}
	}

	return nil
}

// sqliteTxWrapper 包装 sql.Tx 以实现 driver.Tx 接口
type sqliteTxWrapper struct {
	tx *sql.Tx
}

func (t *sqliteTxWrapper) Commit() error {
	return t.tx.Commit()
}

func (t *sqliteTxWrapper) Rollback() error {
	return t.tx.Rollback()
}

// convertArgs 将 driver.Value 转换为 interface{}
func convertArgs(args []driver.Value) []interface{} {
	result := make([]interface{}, len(args))
	for i, v := range args {
		result[i] = v
	}
	return result
}
