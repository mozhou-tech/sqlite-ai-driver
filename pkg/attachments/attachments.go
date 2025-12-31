package attachments

import (
	"fmt"
	"log"
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
	ID           string                 // 文件ID（主键）
	Name         string                 // 文件名
	Size         int64                  // 文件大小（字节）
	ModTime      time.Time              // 修改时间
	DateDir      string                 // 日期目录（YYYY-MM-DD）
	RelativePath string                 // 相对路径（相对于attachments目录）
	AbsolutePath string                 // 绝对路径
	MimeType     string                 // MIME类型
	Metadata     map[string]interface{} // 扩展元数据（JSON）
	CreatedAt    time.Time              // 创建时间
	UpdatedAt    time.Time              // 更新时间
}

// New 创建附件管理器
// workingDir: 工作目录，attachments目录将创建在此目录下
func New(workingDir string) (*Manager, error) {
	log.Printf("[attachments] New called with workingDir: %s", workingDir)

	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to get absolute path: %v", err)
		return nil, fmt.Errorf("获取工作目录绝对路径失败: %w", err)
	}
	log.Printf("[attachments] Absolute working directory: %s", absWorkingDir)

	baseDir := filepath.Join(absWorkingDir, "attachments")
	log.Printf("[attachments] Base directory: %s", baseDir)

	// 确保attachments目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		log.Printf("[attachments] ERROR: failed to create attachments directory: %v", err)
		return nil, fmt.Errorf("创建attachments目录失败: %w", err)
	}
	log.Printf("[attachments] Attachments directory created/verified")

	log.Printf("[attachments] Manager created successfully")
	return &Manager{
		workingDir: absWorkingDir,
		baseDir:    baseDir,
	}, nil
}

// Close 关闭管理器（当前无需操作，保留接口以保持兼容性）
func (m *Manager) Close() error {
	log.Printf("[attachments] Close called")
	return nil
}

// Store 存储文件
// filename: 文件名（不包含路径）
// data: 文件内容
// 返回: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) Store(filename string, data []byte) (string, error) {
	return m.StoreWithMetadata(filename, data, nil, nil)
}

// StoreWithMetadata 存储文件并保存元数据
// filename: 文件名（不包含路径）
// data: 文件内容
// mimeType: MIME类型（可选）
// metadata: 扩展元数据（可选）
// 返回: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) StoreWithMetadata(filename string, data []byte, mimeType *string, metadata map[string]interface{}) (string, error) {
	log.Printf("[attachments] StoreWithMetadata called: filename=%s, size=%d bytes", filename, len(data))

	if filename == "" {
		log.Printf("[attachments] ERROR: filename is empty")
		return "", fmt.Errorf("文件名不能为空")
	}

	// 按日期创建子目录（格式：YYYY-MM-DD）
	dateDir := time.Now().Format("2006-01-02")
	datePath := filepath.Join(m.baseDir, dateDir)
	log.Printf("[attachments] Date directory: %s", dateDir)

	// 确保日期目录存在
	if err := os.MkdirAll(datePath, 0755); err != nil {
		log.Printf("[attachments] ERROR: failed to create date directory: %v", err)
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
		log.Printf("[attachments] File exists, renamed to: %s", filename)
	}

	// 返回文件ID（相对路径）
	fileID := filepath.Join(dateDir, filename)

	// 写入文件
	writeStart := time.Now()
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("[attachments] ERROR: failed to write file: %v (took %v)", err, time.Since(writeStart))
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	log.Printf("[attachments] File written successfully (took %v)", time.Since(writeStart))

	log.Printf("[attachments] StoreWithMetadata completed successfully: fileID=%s", fileID)
	return fileID, nil
}

// Delete 删除文件
// fileID: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) Delete(fileID string) error {
	log.Printf("[attachments] Delete called: fileID=%s", fileID)

	if fileID == "" {
		log.Printf("[attachments] ERROR: fileID is empty")
		return fmt.Errorf("文件ID不能为空")
	}

	filePath := filepath.Join(m.baseDir, fileID)
	log.Printf("[attachments] File path: %s", filePath)

	// 删除文件
	fileStart := time.Now()
	removeErr := os.Remove(filePath)
	if removeErr != nil {
		if os.IsNotExist(removeErr) {
			log.Printf("[attachments] ERROR: file does not exist: %s (took %v)", fileID, time.Since(fileStart))
			return fmt.Errorf("文件不存在: %s", fileID)
		}
		log.Printf("[attachments] ERROR: failed to remove file: %v (took %v)", removeErr, time.Since(fileStart))
		return fmt.Errorf("删除文件失败: %w", removeErr)
	}
	log.Printf("[attachments] File removed successfully (took %v)", time.Since(fileStart))

	log.Printf("[attachments] Delete completed successfully")
	return nil
}

// GetInfo 获取文件基本信息（从文件系统读取）
// fileID: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) GetInfo(fileID string) (*FileInfo, error) {
	log.Printf("[attachments] GetInfo called: fileID=%s", fileID)

	if fileID == "" {
		log.Printf("[attachments] ERROR: fileID is empty")
		return nil, fmt.Errorf("文件ID不能为空")
	}

	// 从文件系统读取
	filePath := filepath.Join(m.baseDir, fileID)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to get absolute path: %v", err)
		return nil, fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 获取文件信息
	fileStart := time.Now()
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[attachments] ERROR: file does not exist: %s (took %v)", fileID, time.Since(fileStart))
			return nil, fmt.Errorf("文件不存在: %s", fileID)
		}
		log.Printf("[attachments] ERROR: failed to stat file: %v (took %v)", err, time.Since(fileStart))
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}
	log.Printf("[attachments] Found in filesystem (took %v)", time.Since(fileStart))

	// 解析日期目录
	dateDir := filepath.Dir(fileID)
	filename := filepath.Base(fileID)

	// 创建 FileInfo（从文件系统读取）
	info := &FileInfo{
		ID:           fileID,
		Name:         filename,
		Size:         fileInfo.Size(),
		ModTime:      fileInfo.ModTime(),
		DateDir:      dateDir,
		RelativePath: fileID,
		AbsolutePath: absPath,
		CreatedAt:    fileInfo.ModTime(),
		UpdatedAt:    fileInfo.ModTime(),
		// MimeType 和 Metadata 不再存储，保持为空
	}

	log.Printf("[attachments] GetInfo completed from filesystem: name=%s, size=%d", info.Name, info.Size)
	return info, nil
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
	log.Printf("[attachments] Read called: fileID=%s", fileID)
	if fileID == "" {
		return nil, fmt.Errorf("文件ID不能为空")
	}

	filePath := filepath.Join(m.baseDir, fileID)
	log.Printf("[attachments] Reading file: %s", filePath)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("[attachments] ERROR: file does not exist: %s", filePath)
		return nil, fmt.Errorf("文件不存在: %s", fileID)
	}

	// 读取文件
	readStart := time.Now()
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to read file: %v (took %v)", err, time.Since(readStart))
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	log.Printf("[attachments] File read successfully: size=%d bytes (took %v)", len(data), time.Since(readStart))

	return data, nil
}

// List 列出指定日期目录下的所有文件（从文件系统读取）
// dateDir: 日期目录（格式：YYYY-MM-DD），如果为空则列出所有日期目录
// 返回: 文件ID列表
func (m *Manager) List(dateDir string) ([]string, error) {
	var fileIDs []string

	if dateDir == "" {
		// 列出所有日期目录
		entries, err := os.ReadDir(m.baseDir)
		if err != nil {
			return nil, fmt.Errorf("读取目录失败: %w", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			// 递归读取子目录中的文件
			subDir := filepath.Join(m.baseDir, entry.Name())
			subEntries, err := os.ReadDir(subDir)
			if err != nil {
				continue
			}
			for _, subEntry := range subEntries {
				if !subEntry.IsDir() {
					fileID := filepath.Join(entry.Name(), subEntry.Name())
					fileIDs = append(fileIDs, fileID)
				}
			}
		}
	} else {
		// 列出指定日期目录下的文件
		datePath := filepath.Join(m.baseDir, dateDir)
		entries, err := os.ReadDir(datePath)
		if err != nil {
			if os.IsNotExist(err) {
				return []string{}, nil
			}
			return nil, fmt.Errorf("读取目录失败: %w", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				fileID := filepath.Join(dateDir, entry.Name())
				fileIDs = append(fileIDs, fileID)
			}
		}
	}

	return fileIDs, nil
}

// ListAll 列出所有文件信息（从文件系统读取）
// dateDir: 日期目录（格式：YYYY-MM-DD），如果为空则列出所有日期目录
// 返回: 文件信息列表
func (m *Manager) ListAll(dateDir string) ([]*FileInfo, error) {
	var files []*FileInfo

	if dateDir == "" {
		// 列出所有日期目录
		entries, err := os.ReadDir(m.baseDir)
		if err != nil {
			return nil, fmt.Errorf("读取目录失败: %w", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			// 递归读取子目录中的文件
			subDir := filepath.Join(m.baseDir, entry.Name())
			subEntries, err := os.ReadDir(subDir)
			if err != nil {
				continue
			}
			for _, subEntry := range subEntries {
				if !subEntry.IsDir() {
					fileID := filepath.Join(entry.Name(), subEntry.Name())
					info, err := m.GetInfo(fileID)
					if err != nil {
						continue
					}
					files = append(files, info)
				}
			}
		}
	} else {
		// 列出指定日期目录下的文件
		datePath := filepath.Join(m.baseDir, dateDir)
		entries, err := os.ReadDir(datePath)
		if err != nil {
			if os.IsNotExist(err) {
				return []*FileInfo{}, nil
			}
			return nil, fmt.Errorf("读取目录失败: %w", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				fileID := filepath.Join(dateDir, entry.Name())
				info, err := m.GetInfo(fileID)
				if err != nil {
					continue
				}
				files = append(files, info)
			}
		}
	}

	return files, nil
}

// GetBaseDir 获取attachments基础目录的绝对路径
func (m *Manager) GetBaseDir() string {
	return m.baseDir
}
