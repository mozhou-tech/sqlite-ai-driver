package sqlite3_driver

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// ext 表示一个 SQLite 扩展
type ext struct {
	lib   string
	entry string
}

// fts5Exts 定义 fts5 扩展的加载路径
var fts5Exts = []ext{
	{"fts5", "sqlite3_fts5_init"},
}

// getDataDir 获取基础数据目录
// 优先从环境变量 DATA_DIR 获取，如果未设置则使用默认值 ./data
func getDataDir() string {
	if dataDir := os.Getenv("DATA_DIR"); dataDir != "" {
		return dataDir
	}
	return "./data"
}

// ensureDataPath 确保数据路径存在，如果是相对路径则自动构建到 db 子目录
func ensureDataPath(path string) (string, error) {
	// 如果路径包含路径分隔符（绝对路径或相对路径），直接使用
	if strings.Contains(path, string(filepath.Separator)) || strings.Contains(path, "/") || strings.Contains(path, "\\") {
		// 确保目录存在
		dir := filepath.Dir(path)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", fmt.Errorf("failed to create directory: %w", err)
			}
		}
		return path, nil
	}

	// 如果是相对路径（不包含路径分隔符），自动构建到 db 子目录
	dataDir := getDataDir()
	fullPath := filepath.Join(dataDir, "db", path)

	// 确保目录存在
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	return fullPath, nil
}

func init() {
	// 创建基础驱动
	baseDriver := &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			// 默认开启 WAL 模式
			// 使用 Exec 方法执行 PRAGMA 语句
			_, err := conn.Exec("PRAGMA journal_mode=WAL;", nil)
			if err != nil {
				return fmt.Errorf("failed to enable WAL mode: %w", err)
			}

			// 加载 fts5 扩展（可选，如果加载失败不影响主功能）
			for _, v := range fts5Exts {
				if err := conn.LoadExtension(v.lib, v.entry); err == nil {
					break
				}
			}

			return nil
		},
	}

	// 注册 sqlite3 驱动（包装后的驱动，支持自动路径处理）
	sql.Register("sqlite-vss", &sqlite3DriverWrapper{driver: baseDriver})
}

// sqlite3DriverWrapper 包装 SQLiteDriver，自动处理路径
type sqlite3DriverWrapper struct {
	driver *sqlite3.SQLiteDriver
}

func (w *sqlite3DriverWrapper) Open(name string) (driver.Conn, error) {
	// 自动构建路径并创建目录
	finalPath, err := ensureDataPath(name)
	if err != nil {
		return nil, err
	}
	return w.driver.Open(finalPath)
}
