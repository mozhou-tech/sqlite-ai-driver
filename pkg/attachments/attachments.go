package attachments

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Manager 附件管理器
type Manager struct {
	workingDir string
	baseDir    string
}

// FileInfo 文件基本信息
type FileInfo struct {
	Name         string    // 文件名
	Size         int64     // 文件大小（字节）
	ModTime      time.Time // 修改时间
	DateDir      string    // 日期目录（YYYY-MM-DD）
	RelativePath string    // 相对路径（相对于attachments目录）
	AbsolutePath string    // 绝对路径
}

// New 创建附件管理器
// workingDir: 工作目录，attachments目录将创建在此目录下
func New(workingDir string) (*Manager, error) {
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, fmt.Errorf("获取工作目录绝对路径失败: %w", err)
	}

	baseDir := filepath.Join(absWorkingDir, "attachments")

	// 确保attachments目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("创建attachments目录失败: %w", err)
	}

	return &Manager{
		workingDir: absWorkingDir,
		baseDir:    baseDir,
	}, nil
}

// Store 存储文件
// filename: 文件名（不包含路径）
// data: 文件内容
// 返回: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) Store(filename string, data []byte) (string, error) {
	if filename == "" {
		return "", fmt.Errorf("文件名不能为空")
	}

	// 按日期创建子目录（格式：YYYY-MM-DD）
	dateDir := time.Now().Format("2006-01-02")
	datePath := filepath.Join(m.baseDir, dateDir)

	// 确保日期目录存在
	if err := os.MkdirAll(datePath, 0755); err != nil {
		return "", fmt.Errorf("创建日期目录失败: %w", err)
	}

	// 构建文件路径
	filePath := filepath.Join(datePath, filename)

	// 如果文件已存在，添加时间戳后缀避免覆盖
	if _, err := os.Stat(filePath); err == nil {
		timestamp := time.Now().Format("150405")
		ext := filepath.Ext(filename)
		nameWithoutExt := filename[:len(filename)-len(ext)]
		filename = fmt.Sprintf("%s_%s%s", nameWithoutExt, timestamp, ext)
		filePath = filepath.Join(datePath, filename)
	}

	// 写入文件
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	// 返回文件ID（相对路径）
	fileID := filepath.Join(dateDir, filename)
	return fileID, nil
}

// StoreFromReader 从Reader存储文件
// filename: 文件名（不包含路径）
// reader: 数据源
// 返回: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) StoreFromReader(filename string, reader io.Reader) (string, error) {
	if filename == "" {
		return "", fmt.Errorf("文件名不能为空")
	}

	// 按日期创建子目录（格式：YYYY-MM-DD）
	dateDir := time.Now().Format("2006-01-02")
	datePath := filepath.Join(m.baseDir, dateDir)

	// 确保日期目录存在
	if err := os.MkdirAll(datePath, 0755); err != nil {
		return "", fmt.Errorf("创建日期目录失败: %w", err)
	}

	// 构建文件路径
	filePath := filepath.Join(datePath, filename)

	// 如果文件已存在，添加时间戳后缀避免覆盖
	if _, err := os.Stat(filePath); err == nil {
		timestamp := time.Now().Format("150405")
		ext := filepath.Ext(filename)
		nameWithoutExt := filename[:len(filename)-len(ext)]
		filename = fmt.Sprintf("%s_%s%s", nameWithoutExt, timestamp, ext)
		filePath = filepath.Join(datePath, filename)
	}

	// 创建文件
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	// 从Reader复制数据
	if _, err := io.Copy(file, reader); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	// 返回文件ID（相对路径）
	fileID := filepath.Join(dateDir, filename)
	return fileID, nil
}

// Delete 删除文件
// fileID: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) Delete(fileID string) error {
	if fileID == "" {
		return fmt.Errorf("文件ID不能为空")
	}

	filePath := filepath.Join(m.baseDir, fileID)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("文件不存在: %s", fileID)
	}

	// 删除文件
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("删除文件失败: %w", err)
	}

	return nil
}

// GetInfo 获取文件基本信息
// fileID: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) GetInfo(fileID string) (*FileInfo, error) {
	if fileID == "" {
		return nil, fmt.Errorf("文件ID不能为空")
	}

	filePath := filepath.Join(m.baseDir, fileID)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 获取文件信息
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("文件不存在: %s", fileID)
		}
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 解析日期目录
	dateDir := filepath.Dir(fileID)
	filename := filepath.Base(fileID)

	return &FileInfo{
		Name:         filename,
		Size:         info.Size(),
		ModTime:      info.ModTime(),
		DateDir:      dateDir,
		RelativePath: fileID,
		AbsolutePath: absPath,
	}, nil
}

// GetAbsolutePath 获取文件的绝对路径
// fileID: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) GetAbsolutePath(fileID string) (string, error) {
	if fileID == "" {
		return "", fmt.Errorf("文件ID不能为空")
	}

	filePath := filepath.Join(m.baseDir, fileID)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 检查文件是否存在
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("文件不存在: %s", fileID)
	}

	return absPath, nil
}

// Read 读取文件内容
// fileID: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) Read(fileID string) ([]byte, error) {
	if fileID == "" {
		return nil, fmt.Errorf("文件ID不能为空")
	}

	filePath := filepath.Join(m.baseDir, fileID)

	// 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("文件不存在: %s", fileID)
		}
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	return data, nil
}

// List 列出指定日期目录下的所有文件
// dateDir: 日期目录（格式：YYYY-MM-DD），如果为空则列出所有日期目录
// 返回: 文件ID列表
func (m *Manager) List(dateDir string) ([]string, error) {
	var searchPath string
	if dateDir == "" {
		searchPath = m.baseDir
	} else {
		searchPath = filepath.Join(m.baseDir, dateDir)
	}

	var fileIDs []string

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 计算相对路径（相对于baseDir）
		relPath, err := filepath.Rel(m.baseDir, path)
		if err != nil {
			return err
		}

		fileIDs = append(fileIDs, relPath)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("遍历文件失败: %w", err)
	}

	return fileIDs, nil
}

// GetBaseDir 获取attachments基础目录的绝对路径
func (m *Manager) GetBaseDir() string {
	return m.baseDir
}
