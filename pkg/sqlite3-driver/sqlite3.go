package sqlite3_driver

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattn/go-sqlite3"
)

// ext 表示一个 SQLite 扩展
type ext struct {
	lib   string
	entry string
}

// vecExts 定义 vector0 扩展的加载路径
var vecExts = []ext{
	{"vector0", "sqlite3_vector_init"},
}

// vssExts 定义 vss0 扩展的加载路径
var vssExts = []ext{
	{"vss0", "sqlite3_vss_init"},
}

// fts5Exts 定义 fts5 扩展的加载路径
var fts5Exts = []ext{
	{"fts5", "sqlite3_fts5_init"},
}

func init() {
	// 从环境变量获取扩展路径
	if e := os.Getenv("SQLITE_VSS_EXT_PATH"); e != "" {
		vecExts = append([]ext{{filepath.Join(e, "vector0"), "sqlite3_vector_init"}}, vecExts...)
		vssExts = append([]ext{{filepath.Join(e, "vss0"), "sqlite3_vss_init"}}, vssExts...)
		fts5Exts = append([]ext{{filepath.Join(e, "fts5"), "sqlite3_fts5_init"}}, fts5Exts...)
	}

	// 注册 sqlite-vss 驱动
	sql.Register("sqlite-vss", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			// 默认开启 WAL 模式
			// 使用 Exec 方法执行 PRAGMA 语句
			_, err := conn.Exec("PRAGMA journal_mode=WAL;", nil)
			if err != nil {
				return fmt.Errorf("failed to enable WAL mode: %w", err)
			}

			// 加载 vector0 扩展
			vecLoaded := false
			var errs []error
			for _, v := range vecExts {
				err := conn.LoadExtension(v.lib, v.entry)
				if err == nil {
					vecLoaded = true
					break
				}
				errs = append(errs, err)
			}
			if !vecLoaded {
				return fmt.Errorf("vector0 extension load error: %w\nhint: the extension must be located in the current directory or in the directory specified by the environment variable SQLITE_VSS_EXT_PATH", errors.Join(errs...))
			}

			// 加载 vss0 扩展
			vssLoaded := false
			errs = nil
			for _, v := range vssExts {
				err := conn.LoadExtension(v.lib, v.entry)
				if err == nil {
					vssLoaded = true
					break
				}
				errs = append(errs, err)
			}
			if !vssLoaded {
				return fmt.Errorf("vss0 extension load error: %w\nhint: the extension must be located in the current directory or in the directory specified by the environment variable SQLITE_VSS_EXT_PATH", errors.Join(errs...))
			}

			// 加载 fts5 扩展（可选，如果加载失败不影响主功能）
			for _, v := range fts5Exts {
				if err := conn.LoadExtension(v.lib, v.entry); err == nil {
					break
				}
			}

			return nil
		},
	})
}
