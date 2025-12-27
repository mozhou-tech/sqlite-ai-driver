package driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

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
	sql.Register("duckdb", &duckdbDriver{})
}

// duckdbDriver 实现了 driver.Driver 接口
type duckdbDriver struct{}

// Open 实现 driver.Driver 接口
func (d *duckdbDriver) Open(name string) (driver.Conn, error) {
	// 使用 NewConnector 创建连接器，并在初始化时安装和加载扩展
	connector, err := duckdb.NewConnector(name, func(execer driver.ExecerContext) error {
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
