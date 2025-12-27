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
		// 解析 URL，确定存储类型
		baseURL, path := url.Split(name, file.Scheme)
		if baseURL == "" {
			// 如果没有指定 scheme，默认为本地文件系统
			localPath = name
			isLocalFile = true
		} else if baseURL == file.Scheme {
			// 本地文件系统
			localPath = path
			// 处理可能的 // 前缀
			if strings.HasPrefix(localPath, "//") {
				localPath = strings.TrimPrefix(localPath, "//")
			}
			isLocalFile = true
		}
		// 如果是远程存储，localPath 保持为空，isLocalFile 为 false
	}

	// 如果是本地文件系统，自动处理路径
	if isLocalFile {
		// 自动构建路径并创建目录
		finalPath, err := ensureDataPath(localPath)
		if err != nil {
			return nil, err
		}

		// 使用 sqlite3 驱动直接打开本地文件
		sqliteDriver := &sqlite3.SQLiteDriver{}
		return sqliteDriver.Open(finalPath)
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
