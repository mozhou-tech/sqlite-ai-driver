package duckdb_driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcboeker/go-duckdb/v2"
)

// 需要安装的扩展列表
var extensions = []string{
	"sqlite", // SQLite 扩展，允许读取和写入 SQLite 数据库
	"vss",    // 向量搜索扩展（Vector Search）
	"fts",    // 全文搜索扩展（Full-Text Search），如果不存在可尝试 "fts"
	"excel",  // Excel 扩展，支持读取和写入 Excel 文件
}

func init() {
	// 注册 duckdb 驱动，默认安装并加载扩展
	// 使用 duckdb.NewConnector 创建连接器，并在初始化时安装和加载扩展
	// 使用 recover 捕获重复注册的 panic（可能 go-duckdb 包本身也注册了）
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 如果是因为重复注册而 panic，忽略它
				errStr := fmt.Sprintf("%v", r)
				if strings.Contains(errStr, "Register called twice for driver duckdb") {
					return
				}
				// 其他 panic 重新抛出
				panic(r)
			}
		}()
		sql.Register("duckdb", &duckdbDriver{})
	}()
}

// duckdbDriver 实现了 driver.Driver 接口
type duckdbDriver struct{}

// getDataDir 获取基础数据目录
// 优先从环境变量 DATA_DIR 获取，如果未设置则使用默认值 ./data
func getDataDir() string {
	if dataDir := os.Getenv("DATA_DIR"); dataDir != "" {
		return dataDir
	}
	return "./data"
}

// ensureDataPath 确保数据路径存在，如果是相对路径则自动构建到 duck 子目录
func ensureDataPath(dsn string) (string, error) {
	// 如果 DSN 包含路径分隔符或已经是完整路径，直接使用
	if strings.Contains(dsn, string(filepath.Separator)) || strings.Contains(dsn, "/") || strings.Contains(dsn, "\\") {
		// 提取路径部分（去掉查询参数）
		pathPart := dsn
		if idx := strings.Index(dsn, "?"); idx != -1 {
			pathPart = dsn[:idx]
		}

		// 确保目录存在
		dir := filepath.Dir(pathPart)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", fmt.Errorf("failed to create directory: %w", err)
			}
		}
		return dsn, nil
	}

	// 如果是相对路径（不包含路径分隔符），自动构建到 duck 子目录
	dataDir := getDataDir()
	// 提取查询参数
	queryPart := ""
	if idx := strings.Index(dsn, "?"); idx != -1 {
		queryPart = dsn[idx:]
		dsn = dsn[:idx]
	}

	fullPath := filepath.Join(dataDir, "duck", dsn) + queryPart

	// 确保目录存在
	dir := filepath.Dir(filepath.Join(dataDir, "duck", dsn))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	return fullPath, nil
}

// ensureReadWriteMode 确保连接字符串默认使用读写模式
// 如果连接字符串中没有 access_mode 参数，则添加 access_mode=read_write
func ensureReadWriteMode(dsn string) string {
	if dsn == "" {
		return "?access_mode=read_write"
	}

	// 检查是否已经包含 access_mode 参数
	if strings.Contains(dsn, "access_mode=") {
		return dsn
	}

	// 如果连接字符串包含 ?，则追加参数；否则添加 ?
	if strings.Contains(dsn, "?") {
		return dsn + "&access_mode=read_write"
	}
	return dsn + "?access_mode=read_write"
}

// Open 实现 driver.Driver 接口
// name 参数可以是：
// - 完整路径：/path/to/duck.db 或 /path/to/duck.db?access_mode=read_write
// - 相对路径（如 "duck.db"）：自动构建到 {DATA_DIR}/duck/ 目录
func (d *duckdbDriver) Open(name string) (driver.Conn, error) {
	// 自动构建路径并创建目录
	finalPath, err := ensureDataPath(name)
	if err != nil {
		return nil, err
	}

	// 确保默认使用读写模式
	dsn := ensureReadWriteMode(finalPath)

	// 使用 NewConnector 创建连接器，并在初始化时安装和加载扩展
	connector, err := duckdb.NewConnector(dsn, func(execer driver.ExecerContext) error {
		// 安装扩展（如果已安装会返回错误，可以忽略）
		for _, ext := range extensions {
			installQuery := fmt.Sprintf("INSTALL %s;", ext)
			_, _ = execer.ExecContext(context.Background(), installQuery, nil)
		}

		// 加载扩展
		var loadErrors []error
		for _, ext := range extensions {
			loadQuery := fmt.Sprintf("LOAD %s;", ext)
			_, err := execer.ExecContext(context.Background(), loadQuery, nil)
			if err != nil {
				loadErrors = append(loadErrors, fmt.Errorf("failed to load extension %s: %w", ext, err))
			}
		}

		// 如果有扩展加载失败，返回错误
		if len(loadErrors) > 0 {
			return fmt.Errorf("extension load errors: %w", errors.Join(loadErrors...))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create connector: %w", err)
	}

	// 创建连接
	conn, err := connector.Connect(context.Background())
	if err != nil {
		connector.Close()
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &duckdbConn{
		conn:      conn,
		connector: connector,
	}, nil
}

// duckdbConn 实现了 driver.Conn 接口
// 注意：connector 的生命周期与连接绑定，连接关闭时也会关闭 connector
type duckdbConn struct {
	conn      driver.Conn
	connector *duckdb.Connector
}

func (c *duckdbConn) Prepare(query string) (driver.Stmt, error) {
	return c.conn.Prepare(query)
}

func (c *duckdbConn) Close() error {
	var errs []error
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.connector != nil {
		if err := c.connector.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (c *duckdbConn) Begin() (driver.Tx, error) {
	return c.conn.Begin()
}
