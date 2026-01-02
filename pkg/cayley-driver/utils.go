package cayley_driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ensureDataPath 确保数据路径存在，如果是相对路径则自动构建到 {workingDir}/data.db
// workingDir: 工作目录，作为基础目录
// path: SQLite3 数据库文件路径
func ensureDataPath(workingDir, path string) (string, error) {
	// 如果路径包含路径分隔符（绝对路径或相对路径），直接使用
	if strings.Contains(path, string(filepath.Separator)) || strings.Contains(path, "/") || strings.Contains(path, "\\") {
		// 确保目录存在
		dir := filepath.Dir(path)
		if dir != "." && dir != "" {
			if err := ensureDir(dir); err != nil {
				return "", fmt.Errorf("failed to create directory: %w", err)
			}
		}
		return path, nil
	}

	// 如果是相对路径（不包含路径分隔符），自动构建到 {workingDir}/data.db
	// 将 workingDir 转换为绝对路径
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for workingDir: %w", err)
	}
	fullPath := filepath.Join(absWorkingDir, "data.db")

	// 确保目录存在
	dir := filepath.Dir(fullPath)
	if err := ensureDir(dir); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	return fullPath, nil
}

// ensureDir 确保目录存在
func ensureDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}
