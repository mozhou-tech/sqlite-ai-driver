package sqlite3_driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
			// 从查询参数中移除 workingDir，因为它不是 SQLite 的参数
			queryParams.Del("workingDir")
		}
	} else {
		dbPath = name
		queryParams = make(url.Values)
	}

	// 自动构建路径并创建目录
	finalPath, err := ensureDataPath(workingDir, dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure data path: %w", err)
	}

	// 构建 DSN，保留原有的查询参数（如 _pragma）
	// 如果没有 _pragma 参数，默认添加 journal_mode(WAL)
	if queryParams.Get("_pragma") == "" {
		queryParams.Set("_pragma", "journal_mode(WAL)")
	}

	dsn := finalPath
	if len(queryParams) > 0 {
		dsn += "?" + queryParams.Encode()
	}

	// 使用已注册的 "sqlite" 驱动打开连接
	// 由于我们不能直接访问已注册的驱动，我们使用 sql.Open 然后获取底层连接
	// 这是一个 workaround，但可以工作
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 获取底层连接
	// 注意：sql.DB 内部维护连接池，我们需要获取一个连接
	// 使用 Conn() 方法获取一个连接
	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// 返回包装的连接，在关闭时同时关闭 db 和 conn
	return &sqliteConnWrapper{db: db, conn: conn}, nil
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
	err1 := c.conn.Close()
	err2 := c.db.Close()
	if err1 != nil {
		return err1
	}
	return err2
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
