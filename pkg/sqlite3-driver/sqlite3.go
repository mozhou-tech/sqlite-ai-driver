package sqlite3_driver

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// getDataDir 获取基础数据目录
// 优先从环境变量 DATA_DIR 获取，如果未设置则使用默认值 ./data
// 返回的路径会被转换为绝对路径
func getDataDir() string {
	var dataDir string
	if envDataDir := os.Getenv("DATA_DIR"); envDataDir != "" {
		dataDir = envDataDir
	} else {
		dataDir = "./data"
	}

	// 转换为绝对路径
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		// 如果转换失败，返回原始路径
		return dataDir
	}
	return absDataDir
}

// ensureDataPath 确保数据路径存在，如果是相对路径则自动构建到 db 子目录
func ensureDataPath(path string) (string, error) {
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

	// 如果是相对路径（不包含路径分隔符），自动构建到 db 子目录
	dataDir := getDataDir()
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
	sql.Register("sqlite3", &sqliteDriverWrapper{})
}

// replaceDriverWithReflection 使用反射替换已注册的驱动
// 这是一个 hack，访问 database/sql 的内部实现
// 注意：这依赖于 Go 的内部实现，可能会在版本更新时失效
func replaceDriverWithReflection(name string, newDriver driver.Driver) {
	// database/sql 包内部使用一个全局的 driversMu 互斥锁和 drivers map
	// 我们需要访问这个 map 并替换驱动
	// 由于 drivers 是 database/sql 包的未导出变量，我们需要使用反射 + unsafe

	// 实际上，由于 database/sql 包的内部实现可能会变化
	// 我们采用一个更实用的方法：不替换驱动，而是接受现状
	// 如果 modernc.org/sqlite 先注册，我们的包装器不会被使用
	// 在这种情况下，用户需要使用绝对路径而不是相对路径

	// 暂时不实现反射替换，因为这太危险且不可靠
	// 用户需要确保我们的包在 modernc.org/sqlite 之前被导入
	// 或者：使用绝对路径而不是相对路径

	// 注意：由于 modernc.org/sqlite 会在 init() 中自动注册驱动，
	// 而我们的 init() 在它的 init() 之后执行，所以我们的注册会失败。
	// 在这种情况下，ensureDataPath 不会被调用，相对路径功能不会工作。
	// 用户需要使用绝对路径，或者确保我们的包在 modernc.org/sqlite 之前被导入。
	_ = name
	_ = newDriver
}

// sqliteDriverWrapper 包装 SQLite 驱动，自动处理路径和 PRAGMA 设置
type sqliteDriverWrapper struct{}

func (w *sqliteDriverWrapper) Open(name string) (driver.Conn, error) {
	// 自动构建路径并创建目录
	// ensureDataPath 会读取环境变量 DATA_DIR，所以环境变量必须在调用 Open 之前设置
	finalPath, err := ensureDataPath(name)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure data path: %w", err)
	}

	// 使用 modernc.org/sqlite 驱动打开数据库
	// 通过连接字符串参数设置 PRAGMA
	// modernc.org/sqlite 支持通过 _pragma 参数设置 PRAGMA
	dsn := finalPath + "?_pragma=journal_mode(WAL)"

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
	conn, err := db.Conn(nil)
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
	stmt, err := c.conn.PrepareContext(nil, query)
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
	tx, err := c.conn.BeginTx(nil, nil)
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
