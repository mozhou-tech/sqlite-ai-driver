package file_driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/viant/afs"
	"github.com/viant/afs/file"
	"github.com/viant/afs/url"
	_ "modernc.org/sqlite"
)

func init() {
	// 注册 file 驱动，支持从各种存储后端读取 SQLite 数据库文件
	sql.Register("file", &fileDriver{})
}

// fileDriver 实现了 driver.Driver 接口
// 使用 afs 抽象文件系统支持从本地文件系统、S3、GCS 等存储后端读取文件
type fileDriver struct{}

// getDataDir 获取基础数据目录
// 优先从环境变量 DATA_DIR 获取，如果未设置则使用默认值 ./data
func getDataDir() string {
	if dataDir := os.Getenv("DATA_DIR"); dataDir != "" {
		return dataDir
	}
	return "./data"
}

// ensureDataPath 确保数据路径存在，如果是相对路径则自动构建到 files 子目录
func ensureDataPath(path string) (string, error) {
	// 如果路径包含路径分隔符（绝对路径或相对路径），需要处理
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

	// 如果是相对路径（不包含路径分隔符），自动构建到 files 子目录
	dataDir := getDataDir()
	fullPath := filepath.Join(dataDir, "files", path)

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

// Open 实现 driver.Driver 接口
// name 参数可以是：
// - 本地文件路径：file:///path/to/db.sqlite 或 /path/to/db.sqlite
// - 相对路径（如 "files.db"）：自动构建到 {DATA_DIR}/files/ 目录
// - S3 路径：s3://bucket/path/to/db.sqlite
// - GCS 路径：gs://bucket/path/to/db.sqlite
// - 其他 afs 支持的存储后端
func (d *fileDriver) Open(name string) (driver.Conn, error) {
	// 如果 name 为空，返回错误
	if name == "" {
		return nil, fmt.Errorf("file driver: data source name cannot be empty")
	}

	// 创建 afs 文件系统服务
	fs := afs.New()

	// 检查是否是 file:// 协议或本地文件路径
	var localPath string
	isLocalFile := false

	if strings.HasPrefix(name, "file://") {
		// 处理 file:// 协议
		localPath = strings.TrimPrefix(name, "file://")
		// 处理 file:///absolute/path 格式（三个斜杠），移除前两个斜杠
		if strings.HasPrefix(localPath, "///") {
			localPath = strings.TrimPrefix(localPath, "//")
		} else if strings.HasPrefix(localPath, "//") {
			// file:////path 格式（四个斜杠），移除前两个
			localPath = strings.TrimPrefix(localPath, "//")
		}
		isLocalFile = true
	} else {
		// 检查是否是已知的远程存储 scheme
		isRemoteScheme := strings.HasPrefix(name, "s3://") ||
			strings.HasPrefix(name, "gs://") ||
			strings.HasPrefix(name, "http://") ||
			strings.HasPrefix(name, "https://")

		if isRemoteScheme {
			// 远程存储，localPath 保持为空，isLocalFile 为 false
			isLocalFile = false
		} else {
			// 解析 URL，确定存储类型
			baseURL, path := url.Split(name, file.Scheme)
			if baseURL == "" || baseURL == file.Scheme {
				// 如果没有指定 scheme 或者是 file scheme，默认为本地文件系统
				if baseURL == file.Scheme {
					localPath = path
					// 处理可能的 // 前缀
					if strings.HasPrefix(localPath, "//") {
						localPath = strings.TrimPrefix(localPath, "//")
					}
				} else {
					localPath = name
				}
				isLocalFile = true
			} else {
				// 其他未知的 scheme，尝试作为本地文件处理
				localPath = name
				isLocalFile = true
			}
		}
	}

	// 如果是本地文件系统，自动处理路径
	if isLocalFile {
		// 自动构建路径并创建目录
		finalPath, err := ensureDataPath(localPath)
		if err != nil {
			return nil, err
		}

		// 使用 modernc.org/sqlite 驱动直接打开本地文件
		// 通过连接字符串参数设置 PRAGMA
		dsn := finalPath + "?_pragma=journal_mode(WAL)"
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		conn, err := db.Conn(context.Background())
		if err != nil {
			db.Close()
			return nil, err
		}
		return &fileConnWrapper{db: db, conn: conn}, nil
	}

	// 对于远程存储（S3、GCS 等），需要先下载到临时文件
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "sqlite-*.db")
	if err != nil {
		return nil, fmt.Errorf("file driver: failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	// 从远程存储下载文件
	reader, err := fs.OpenURL(context.Background(), name)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("file driver: failed to open remote file %s: %w", name, err)
	}
	defer reader.Close()

	// 将远程文件内容写入临时文件
	tmpFile, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("file driver: failed to open temp file for writing: %w", err)
	}

	_, err = io.Copy(tmpFile, reader)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("file driver: failed to copy remote file to temp: %w", err)
	}

	// 使用 modernc.org/sqlite 驱动打开临时文件
	// 通过连接字符串参数设置 PRAGMA
	dsn := tmpPath + "?_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("file driver: failed to open sqlite connection: %w", err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("file driver: failed to get connection: %w", err)
	}

	// 返回包装的连接，在关闭时清理临时文件
	return &fileConnWrapper{
		db:      db,
		conn:    conn,
		tmpPath: tmpPath,
	}, nil
}

// fileConnWrapper 包装了 sql.DB 和 sql.Conn，在关闭时清理临时文件
type fileConnWrapper struct {
	db      *sql.DB
	conn    *sql.Conn
	tmpPath string
}

func (c *fileConnWrapper) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.conn.PrepareContext(context.Background(), query)
	if err != nil {
		return nil, err
	}
	return &fileStmtWrapper{stmt: stmt}, nil
}

func (c *fileConnWrapper) Close() error {
	err1 := c.conn.Close()
	err2 := c.db.Close()
	if c.tmpPath != "" {
		if rmErr := os.Remove(c.tmpPath); rmErr != nil && err1 == nil && err2 == nil {
			err1 = rmErr
		}
	}
	if err1 != nil {
		return err1
	}
	return err2
}

func (c *fileConnWrapper) Begin() (driver.Tx, error) {
	tx, err := c.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	return &fileTxWrapper{tx: tx}, nil
}

// fileStmtWrapper 包装 sql.Stmt 以实现 driver.Stmt 接口
type fileStmtWrapper struct {
	stmt *sql.Stmt
}

func (s *fileStmtWrapper) Close() error {
	return s.stmt.Close()
}

func (s *fileStmtWrapper) NumInput() int {
	return -1
}

func (s *fileStmtWrapper) Exec(args []driver.Value) (driver.Result, error) {
	return s.stmt.Exec(convertFileArgs(args)...)
}

func (s *fileStmtWrapper) Query(args []driver.Value) (driver.Rows, error) {
	rows, err := s.stmt.Query(convertFileArgs(args)...)
	if err != nil {
		return nil, err
	}
	return &fileRowsWrapper{rows: rows}, nil
}

// fileTxWrapper 包装 sql.Tx 以实现 driver.Tx 接口
type fileTxWrapper struct {
	tx *sql.Tx
}

func (t *fileTxWrapper) Commit() error {
	return t.tx.Commit()
}

func (t *fileTxWrapper) Rollback() error {
	return t.tx.Rollback()
}

// fileRowsWrapper 包装 sql.Rows 以实现 driver.Rows 接口
type fileRowsWrapper struct {
	rows *sql.Rows
}

func (r *fileRowsWrapper) Columns() []string {
	cols, err := r.rows.Columns()
	if err != nil {
		return nil
	}
	return cols
}

func (r *fileRowsWrapper) Close() error {
	return r.rows.Close()
}

func (r *fileRowsWrapper) Next(dest []driver.Value) error {
	if !r.rows.Next() {
		return r.rows.Err()
	}
	cols := r.Columns()
	if len(dest) < len(cols) {
		return fmt.Errorf("not enough destination values")
	}
	values := make([]interface{}, len(cols))
	for i := range values {
		values[i] = new(interface{})
	}
	if err := r.rows.Scan(values...); err != nil {
		return err
	}
	for i, v := range values {
		if ptr, ok := v.(*interface{}); ok {
			dest[i] = *ptr
		} else {
			dest[i] = v
		}
	}
	return nil
}

// convertFileArgs 将 driver.Value 转换为 interface{}
func convertFileArgs(args []driver.Value) []interface{} {
	result := make([]interface{}, len(args))
	for i, v := range args {
		result[i] = v
	}
	return result
}
