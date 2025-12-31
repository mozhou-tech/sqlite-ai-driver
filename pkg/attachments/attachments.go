package attachments

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
)

// Manager 附件管理器
type Manager struct {
	workingDir string
	baseDir    string
	db         *sql.DB
}

// FileInfo 文件基本信息
type FileInfo struct {
	ID           string                 // 文件ID（主键）
	Name         string                 // 文件名
	Size         int64                  // 文件大小（字节）
	ModTime      time.Time              // 修改时间
	DateDir      string                 // 日期目录（YYYYMMDD）
	RelativePath string                 // 相对路径（相对于attachments目录）
	AbsolutePath string                 // 绝对路径
	MimeType     string                 // MIME类型
	MD5          string                 // 文件MD5值
	Metadata     map[string]interface{} // 扩展元数据（JSON）
	CreatedAt    time.Time              // 创建时间
	UpdatedAt    time.Time              // 更新时间
}

// generateRandomString 生成指定长度的随机字符串（使用小写字母和数字）
func generateRandomString(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	for i := range bytes {
		bytes[i] = charset[bytes[i]%byte(len(charset))]
	}
	return string(bytes), nil
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

	// 初始化数据库连接
	dbPath := "attachments.db"
	dsn := fmt.Sprintf("%s?workingDir=%s", dbPath, absWorkingDir)
	log.Printf("[attachments] Opening database: %s", dsn)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to open database: %v", err)
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 设置连接池参数
	db.SetMaxIdleConns(1)
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Printf("[attachments] ERROR: failed to ping database: %v", err)
		db.Close()
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	// 创建表
	if err := createTable(ctx, db); err != nil {
		log.Printf("[attachments] ERROR: failed to create table: %v", err)
		db.Close()
		return nil, fmt.Errorf("创建表失败: %w", err)
	}
	log.Printf("[attachments] Database table created/verified")

	log.Printf("[attachments] Manager created successfully")
	return &Manager{
		workingDir: absWorkingDir,
		baseDir:    baseDir,
		db:         db,
	}, nil
}

// createTable 创建文件元数据表
func createTable(ctx context.Context, db *sql.DB) error {
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS attachments_metadata (
			file_id TEXT PRIMARY KEY,
			relative_path TEXT NOT NULL,
			absolute_path TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			mime_type TEXT,
			md5 TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)
	`
	_, err := db.ExecContext(ctx, createTableSQL)
	return err
}

// Close 关闭管理器
func (m *Manager) Close() error {
	log.Printf("[attachments] Close called")
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

// Store 存储文件
// filename: 文件名（不包含路径）
// data: 文件内容
// 返回: 文件ID（相对路径，格式：YYYYMMDD/filename）
func (m *Manager) Store(filename string, data []byte) (string, error) {
	return m.StoreWithMetadata(filename, data, nil, nil)
}

// StoreWithMetadata 存储文件并保存元数据
// filename: 文件名（不包含路径）
// data: 文件内容
// mimeType: MIME类型（可选）
// metadata: 扩展元数据（可选）
// 返回: 文件ID（相对路径，格式：YYYYMMDD/filename）
func (m *Manager) StoreWithMetadata(filename string, data []byte, mimeType *string, metadata map[string]interface{}) (string, error) {
	log.Printf("[attachments] StoreWithMetadata called: filename=%s, size=%d bytes", filename, len(data))

	if filename == "" {
		log.Printf("[attachments] ERROR: filename is empty")
		return "", fmt.Errorf("文件名不能为空")
	}

	// 按日期创建子目录（格式：YYYYMMDD）
	dateDir := time.Now().Format("20060102")
	datePath := filepath.Join(m.baseDir, dateDir)
	log.Printf("[attachments] Date directory: %s", dateDir)

	// 确保日期目录存在
	if err := os.MkdirAll(datePath, 0755); err != nil {
		log.Printf("[attachments] ERROR: failed to create date directory: %v", err)
		return "", fmt.Errorf("创建日期目录失败: %w", err)
	}

	// 生成6位随机字符串并添加到文件名前面
	randomStr, err := generateRandomString(6)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to generate random string: %v", err)
		return "", fmt.Errorf("生成随机字符串失败: %w", err)
	}
	filename = fmt.Sprintf("%s_%s", randomStr, filename)
	log.Printf("[attachments] Filename with random prefix: %s", filename)

	// 构建文件路径
	filePath := filepath.Join(datePath, filename)

	// 返回文件ID（相对路径）
	fileID := filepath.Join(dateDir, filename)

	// 计算文件绝对路径
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to get absolute path: %v", err)
		return "", fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 计算MD5
	md5Hash := md5.Sum(data)
	md5Str := hex.EncodeToString(md5Hash[:])

	// 写入文件
	writeStart := time.Now()
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("[attachments] ERROR: failed to write file: %v (took %v)", err, time.Since(writeStart))
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	log.Printf("[attachments] File written successfully (took %v)", time.Since(writeStart))

	// 存储元数据到数据库
	now := time.Now()
	mimeTypeStr := ""
	if mimeType != nil {
		mimeTypeStr = *mimeType
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	insertSQL := `
		INSERT OR REPLACE INTO attachments_metadata 
		(file_id, relative_path, absolute_path, file_size, mime_type, md5, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err = m.db.ExecContext(ctx, insertSQL, fileID, fileID, absPath, len(data), mimeTypeStr, md5Str, now, now)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to insert metadata: %v", err)
		// 如果数据库插入失败，删除已写入的文件
		os.Remove(filePath)
		return "", fmt.Errorf("存储元数据失败: %w", err)
	}
	log.Printf("[attachments] Metadata stored successfully")

	log.Printf("[attachments] StoreWithMetadata completed successfully: fileID=%s", fileID)
	return fileID, nil
}

// StoreFromFile 从文件路径存储文件
// filePath: 源文件路径
// 返回: 文件ID（相对路径，格式：YYYYMMDD/filename）
func (m *Manager) StoreFromFile(filePath string) (string, error) {
	return m.StoreFromFileWithMetadata(filePath, nil, nil)
}

// StoreFromFileWithMetadata 从文件路径存储文件并保存元数据
// filePath: 源文件路径
// mimeType: MIME类型（可选）
// metadata: 扩展元数据（可选）
// 返回: 文件ID（相对路径，格式：YYYYMMDD/filename）
func (m *Manager) StoreFromFileWithMetadata(filePath string, mimeType *string, metadata map[string]interface{}) (string, error) {
	log.Printf("[attachments] StoreFromFileWithMetadata called: filePath=%s", filePath)

	if filePath == "" {
		log.Printf("[attachments] ERROR: filePath is empty")
		return "", fmt.Errorf("文件路径不能为空")
	}

	// 打开源文件
	srcFile, err := os.Open(filePath)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to open source file: %v", err)
		return "", fmt.Errorf("打开源文件失败: %w", err)
	}
	defer srcFile.Close()

	// 从路径中提取文件名
	filename := filepath.Base(filePath)
	if filename == "" || filename == "." || filename == "/" {
		log.Printf("[attachments] ERROR: invalid filename from path: %s", filePath)
		return "", fmt.Errorf("无法从路径提取文件名: %s", filePath)
	}

	// 按日期创建子目录（格式：YYYYMMDD）
	dateDir := time.Now().Format("20060102")
	datePath := filepath.Join(m.baseDir, dateDir)
	log.Printf("[attachments] Date directory: %s", dateDir)

	// 确保日期目录存在
	if err := os.MkdirAll(datePath, 0755); err != nil {
		log.Printf("[attachments] ERROR: failed to create date directory: %v", err)
		return "", fmt.Errorf("创建日期目录失败: %w", err)
	}

	// 生成6位随机字符串并添加到文件名前面
	randomStr, err := generateRandomString(6)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to generate random string: %v", err)
		return "", fmt.Errorf("生成随机字符串失败: %w", err)
	}
	filename = fmt.Sprintf("%s_%s", randomStr, filename)
	log.Printf("[attachments] Filename with random prefix: %s", filename)

	// 构建目标文件路径
	dstFilePath := filepath.Join(datePath, filename)

	// 返回文件ID（相对路径）
	fileID := filepath.Join(dateDir, filename)

	// 计算文件绝对路径
	absPath, err := filepath.Abs(dstFilePath)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to get absolute path: %v", err)
		return "", fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 创建目标文件
	dstFile, err := os.Create(dstFilePath)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to create destination file: %v", err)
		return "", fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer dstFile.Close()

	// 使用 io.TeeReader 在复制时同时计算MD5
	hash := md5.New()
	teeReader := io.TeeReader(srcFile, hash)

	// 使用 io.Copy 复制文件
	copyStart := time.Now()
	written, err := io.Copy(dstFile, teeReader)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to copy file: %v (took %v)", err, time.Since(copyStart))
		os.Remove(dstFilePath) // 删除已创建的目标文件
		return "", fmt.Errorf("复制文件失败: %w", err)
	}
	log.Printf("[attachments] File copied successfully: size=%d bytes (took %v)", written, time.Since(copyStart))

	// 计算MD5
	md5Hash := hash.Sum(nil)
	md5Str := hex.EncodeToString(md5Hash)

	// 确保目标文件已刷新到磁盘
	if err := dstFile.Sync(); err != nil {
		log.Printf("[attachments] WARNING: failed to sync file: %v", err)
	}

	// 存储元数据到数据库
	now := time.Now()
	mimeTypeStr := ""
	if mimeType != nil {
		mimeTypeStr = *mimeType
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	insertSQL := `
		INSERT OR REPLACE INTO attachments_metadata 
		(file_id, relative_path, absolute_path, file_size, mime_type, md5, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err = m.db.ExecContext(ctx, insertSQL, fileID, fileID, absPath, written, mimeTypeStr, md5Str, now, now)
	if err != nil {
		log.Printf("[attachments] ERROR: failed to insert metadata: %v", err)
		// 如果数据库插入失败，删除已写入的文件
		os.Remove(dstFilePath)
		return "", fmt.Errorf("存储元数据失败: %w", err)
	}
	log.Printf("[attachments] Metadata stored successfully")

	log.Printf("[attachments] StoreFromFileWithMetadata completed successfully: fileID=%s", fileID)
	return fileID, nil
}

// Delete 删除文件
// fileID: 文件ID（相对路径，格式：YYYYMMDD/filename）
func (m *Manager) Delete(fileID string) error {
	log.Printf("[attachments] Delete called: fileID=%s", fileID)

	if fileID == "" {
		log.Printf("[attachments] ERROR: fileID is empty")
		return fmt.Errorf("文件ID不能为空")
	}

	filePath := filepath.Join(m.baseDir, fileID)
	log.Printf("[attachments] File path: %s", filePath)

	// 从数据库删除元数据
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	deleteSQL := `DELETE FROM attachments_metadata WHERE file_id = ?`
	dbStart := time.Now()
	_, err := m.db.ExecContext(ctx, deleteSQL, fileID)
	if err != nil {
		log.Printf("[attachments] WARNING: failed to delete metadata from database: %v (took %v)", err, time.Since(dbStart))
		// 继续删除文件，不因为数据库错误而失败
	} else {
		log.Printf("[attachments] Metadata deleted from database (took %v)", time.Since(dbStart))
	}

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

// GetInfo 获取文件基本信息（优先从数据库读取，如果不存在则从文件系统读取）
// fileID: 文件ID（相对路径，格式：YYYYMMDD/filename）
func (m *Manager) GetInfo(fileID string) (*FileInfo, error) {
	log.Printf("[attachments] GetInfo called: fileID=%s", fileID)

	if fileID == "" {
		log.Printf("[attachments] ERROR: fileID is empty")
		return nil, fmt.Errorf("文件ID不能为空")
	}

	// 先从数据库读取
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	querySQL := `
		SELECT relative_path, absolute_path, file_size, mime_type, md5, created_at, updated_at
		FROM attachments_metadata
		WHERE file_id = ?
	`

	var relativePath, absolutePath, mimeType, md5Str string
	var fileSize int64
	var createdAt, updatedAt time.Time

	dbStart := time.Now()
	err := m.db.QueryRowContext(ctx, querySQL, fileID).Scan(
		&relativePath, &absolutePath, &fileSize, &mimeType, &md5Str, &createdAt, &updatedAt,
	)

	if err == nil {
		// 从数据库读取成功
		log.Printf("[attachments] Found in database (took %v)", time.Since(dbStart))

		// 解析日期目录和文件名
		dateDir := filepath.Dir(fileID)
		filename := filepath.Base(fileID)

		// 获取文件系统信息以获取 ModTime
		filePath := filepath.Join(m.baseDir, fileID)
		fileInfo, err := os.Stat(filePath)
		modTime := createdAt
		if err == nil {
			modTime = fileInfo.ModTime()
		}

		info := &FileInfo{
			ID:           fileID,
			Name:         filename,
			Size:         fileSize,
			ModTime:      modTime,
			DateDir:      dateDir,
			RelativePath: relativePath,
			AbsolutePath: absolutePath,
			MimeType:     mimeType,
			MD5:          md5Str,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
		}

		log.Printf("[attachments] GetInfo completed from database: name=%s, size=%d, md5=%s", info.Name, info.Size, info.MD5)
		return info, nil
	}

	if err != sql.ErrNoRows {
		log.Printf("[attachments] ERROR: failed to query database: %v (took %v)", err, time.Since(dbStart))
		// 数据库查询出错，继续尝试从文件系统读取
	}

	// 数据库中没有记录，从文件系统读取
	log.Printf("[attachments] Not found in database, reading from filesystem")
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
		// MimeType 和 MD5 从文件系统无法获取，保持为空
	}

	log.Printf("[attachments] GetInfo completed from filesystem: name=%s, size=%d", info.Name, info.Size)
	return info, nil
}

// GetAbsolutePath 获取文件的绝对路径
// fileID: 文件ID（相对路径，格式：YYYYMMDD/filename）
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
// fileID: 文件ID（相对路径，格式：YYYYMMDD/filename）
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
// dateDir: 日期目录（格式：YYYYMMDD），如果为空则列出所有日期目录
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
// dateDir: 日期目录（格式：YYYYMMDD），如果为空则列出所有日期目录
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
