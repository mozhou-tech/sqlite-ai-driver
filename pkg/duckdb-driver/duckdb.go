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
	"fts",    // 全文搜索扩展（Full-Text Search）
	"json",   // JSON 扩展，支持 JSON 类型和函数
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

// ensureDataPath 确保数据路径存在，所有路径都统一映射到共享数据库文件 ./data/indexing/all.db
// 无论输入是相对路径还是绝对路径，都会映射到同一个共享数据库文件
// 不同的业务模块应使用不同的表名前缀来区分（如 lightrag_、imagesearch_）
func ensureDataPath(dsn string) (string, error) {
	dataDir := "./data"

	// 提取查询参数（如果有）
	queryPart := ""
	if idx := strings.Index(dsn, "?"); idx != -1 {
		queryPart = dsn[idx:]
	}

	// 统一映射到共享数据库文件 ./data/indexing/all.db
	fullPath := filepath.Join(dataDir, "indexing", "all.db") + queryPart

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
// name 参数可以是任意路径（相对或绝对），但都会被统一映射到共享数据库文件 ./data/indexing/all.db
// 不同的业务模块应使用不同的表名前缀来区分（如 lightrag_、imagesearch_）
// 查询参数会被保留（如 ?access_mode=read_write）
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
