package file_driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-sqlite3"
	"github.com/viant/afs"
	"github.com/viant/afs/file"
	"github.com/viant/afs/url"
)

func init() {
	// 注册 file 驱动，支持从各种存储后端读取 SQLite 数据库文件
	sql.Register("file", &fileDriver{})
}

// fileDriver 实现了 driver.Driver 接口
// 使用 afs 抽象文件系统支持从本地文件系统、S3、GCS 等存储后端读取文件
type fileDriver struct{}

// Open 实现 driver.Driver 接口
// name 参数可以是：
// - 本地文件路径：file:///path/to/db.sqlite 或 /path/to/db.sqlite
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

	// 解析 URL，确定存储类型
	baseURL, path := url.Split(name, file.Scheme)
	if baseURL == "" {
		// 如果没有指定 scheme，默认为本地文件系统
		baseURL = file.Scheme
		path = name
	}

	// 如果是本地文件系统，直接使用 sqlite3 驱动
	if baseURL == file.Scheme {
		// 移除 file:// 前缀（如果有）
		localPath := strings.TrimPrefix(path, "//")
		if localPath == "" {
			localPath = path
		}

		// 使用 sqlite3 驱动直接打开本地文件
		sqliteDriver := &sqlite3.SQLiteDriver{}
		return sqliteDriver.Open(localPath)
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

	// 使用 sqlite3 驱动打开临时文件
	sqliteDriver := &sqlite3.SQLiteDriver{}
	conn, err := sqliteDriver.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("file driver: failed to open sqlite3 connection: %w", err)
	}

	// 返回包装的连接，在关闭时清理临时文件
	return &fileConn{
		Conn:    conn,
		tmpPath: tmpPath,
	}, nil
}

// fileConn 包装了 driver.Conn，在关闭时清理临时文件
type fileConn struct {
	driver.Conn
	tmpPath string
}

func (c *fileConn) Close() error {
	err := c.Conn.Close()
	if c.tmpPath != "" {
		if rmErr := os.Remove(c.tmpPath); rmErr != nil && err == nil {
			err = rmErr
		}
	}
	return err
}
