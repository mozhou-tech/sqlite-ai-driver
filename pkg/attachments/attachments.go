package attachments

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, fmt.Errorf("获取工作目录绝对路径失败: %w", err)
	}

	baseDir := filepath.Join(absWorkingDir, "attachments")

	// 确保attachments目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("创建attachments目录失败: %w", err)
	}

	// 打开SQLite数据库（使用sqlite3-driver，会自动处理路径）
	dbPath := "attachments.db"
	db, err := sql.Open("sqlite3", dbPath+"?workingDir="+absWorkingDir)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 设置连接池参数
	db.SetMaxIdleConns(10)
	db.SetMaxOpenConns(100)
	db.SetConnMaxLifetime(time.Hour)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	// 创建表
	if err := createTable(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建表失败: %w", err)
	}

	return &Manager{
		workingDir: absWorkingDir,
		baseDir:    baseDir,
		db:         db,
	}, nil
}

// createTable 创建文件元数据表
func createTable(ctx context.Context, db *sql.DB) error {
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS attachments (
			id TEXT PRIMARY KEY,
			filename TEXT NOT NULL,
			size INTEGER NOT NULL,
			date_dir TEXT NOT NULL,
			relative_path TEXT NOT NULL UNIQUE,
			absolute_path TEXT NOT NULL,
			mime_type TEXT,
			metadata TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`

	_, err := db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("执行创建表SQL失败: %w", err)
	}

	// 创建索引
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_attachments_date_dir ON attachments(date_dir)",
		"CREATE INDEX IF NOT EXISTS idx_attachments_created_at ON attachments(created_at)",
		"CREATE INDEX IF NOT EXISTS idx_attachments_filename ON attachments(filename)",
	}

	for _, indexSQL := range indexes {
		if _, err := db.ExecContext(ctx, indexSQL); err != nil {
			return fmt.Errorf("创建索引失败: %w", err)
		}
	}

	return nil
}

// Close 关闭数据库连接
func (m *Manager) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
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

	// 获取文件信息
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 返回文件ID（相对路径）
	fileID := filepath.Join(dateDir, filename)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 序列化元数据
	var metadataJSON string
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return "", fmt.Errorf("序列化元数据失败: %w", err)
		}
		metadataJSON = string(metadataBytes)
	}

	// 保存到数据库
	ctx := context.Background()
	now := time.Now()
	insertSQL := `
		INSERT INTO attachments (
			id, filename, size, date_dir, relative_path, absolute_path,
			mime_type, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = m.db.ExecContext(ctx, insertSQL,
		fileID, filename, fileInfo.Size(), dateDir, fileID, absPath,
		mimeType, metadataJSON, now, now,
	)
	if err != nil {
		// 如果数据库插入失败，删除已创建的文件
		os.Remove(filePath)
		return "", fmt.Errorf("保存文件元数据到数据库失败: %w", err)
	}

	return fileID, nil
}

// StoreFromReader 从Reader存储文件
// filename: 文件名（不包含路径）
// reader: 数据源
// 返回: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) StoreFromReader(filename string, reader io.Reader) (string, error) {
	return m.StoreFromReaderWithMetadata(filename, reader, nil, nil)
}

// StoreFromReaderWithMetadata 从Reader存储文件并保存元数据
// filename: 文件名（不包含路径）
// reader: 数据源
// mimeType: MIME类型（可选）
// metadata: 扩展元数据（可选）
// 返回: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) StoreFromReaderWithMetadata(filename string, reader io.Reader, mimeType *string, metadata map[string]interface{}) (string, error) {
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
	size, err := io.Copy(file, reader)
	if err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	// 返回文件ID（相对路径）
	fileID := filepath.Join(dateDir, filename)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 序列化元数据
	var metadataJSON string
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			os.Remove(filePath)
			return "", fmt.Errorf("序列化元数据失败: %w", err)
		}
		metadataJSON = string(metadataBytes)
	}

	// 保存到数据库
	ctx := context.Background()
	now := time.Now()
	insertSQL := `
		INSERT INTO attachments (
			id, filename, size, date_dir, relative_path, absolute_path,
			mime_type, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = m.db.ExecContext(ctx, insertSQL,
		fileID, filename, size, dateDir, fileID, absPath,
		mimeType, metadataJSON, now, now,
	)
	if err != nil {
		// 如果数据库插入失败，删除已创建的文件
		os.Remove(filePath)
		return "", fmt.Errorf("保存文件元数据到数据库失败: %w", err)
	}

	return fileID, nil
}

// Delete 删除文件
// fileID: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) Delete(fileID string) error {
	if fileID == "" {
		return fmt.Errorf("文件ID不能为空")
	}

	filePath := filepath.Join(m.baseDir, fileID)

	// 删除数据库记录
	ctx := context.Background()
	deleteSQL := `DELETE FROM attachments WHERE id = ?`
	result, err := m.db.ExecContext(ctx, deleteSQL, fileID)
	if err != nil {
		return fmt.Errorf("删除数据库记录失败: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("获取删除行数失败: %w", err)
	}

	// 删除文件（即使数据库中没有记录也尝试删除文件）
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除文件失败: %w", err)
	}

	// 如果数据库和文件系统中都不存在，返回错误
	if rowsAffected == 0 {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			return fmt.Errorf("文件不存在: %s", fileID)
		}
	}

	return nil
}

// GetInfo 获取文件基本信息（优先从数据库读取）
// fileID: 文件ID（相对路径，格式：YYYY-MM-DD/filename）
func (m *Manager) GetInfo(fileID string) (*FileInfo, error) {
	if fileID == "" {
		return nil, fmt.Errorf("文件ID不能为空")
	}

	ctx := context.Background()

	// 先从数据库读取
	querySQL := `
		SELECT id, filename, size, date_dir, relative_path, absolute_path,
		       mime_type, metadata, created_at, updated_at
		FROM attachments
		WHERE id = ?
	`

	var info FileInfo
	var mimeType sql.NullString
	var metadataJSON sql.NullString
	var createdAt, updatedAt time.Time

	err := m.db.QueryRowContext(ctx, querySQL, fileID).Scan(
		&info.ID, &info.Name, &info.Size, &info.DateDir, &info.RelativePath,
		&info.AbsolutePath, &mimeType, &metadataJSON, &createdAt, &updatedAt,
	)

	if err == nil {
		// 数据库中有记录
		info.ModTime = updatedAt
		info.CreatedAt = createdAt
		info.UpdatedAt = updatedAt
		if mimeType.Valid {
			info.MimeType = mimeType.String
		}
		if metadataJSON.Valid && metadataJSON.String != "" {
			if err := json.Unmarshal([]byte(metadataJSON.String), &info.Metadata); err != nil {
				// 如果解析失败，忽略元数据
				info.Metadata = nil
			}
		}

		// 验证文件是否仍然存在
		if _, err := os.Stat(info.AbsolutePath); os.IsNotExist(err) {
			// 文件不存在，但数据库中有记录，返回数据库信息但标记为警告
			// 或者可以选择从数据库删除该记录
		}

		return &info, nil
	}

	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("查询数据库失败: %w", err)
	}

	// 数据库中没有记录，从文件系统读取
	filePath := filepath.Join(m.baseDir, fileID)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 获取文件信息
	fileInfo, err := os.Stat(filePath)
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
		ID:           fileID,
		Name:         filename,
		Size:         fileInfo.Size(),
		ModTime:      fileInfo.ModTime(),
		DateDir:      dateDir,
		RelativePath: fileID,
		AbsolutePath: absPath,
		CreatedAt:    fileInfo.ModTime(),
		UpdatedAt:    fileInfo.ModTime(),
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

// List 列出指定日期目录下的所有文件（从数据库读取）
// dateDir: 日期目录（格式：YYYY-MM-DD），如果为空则列出所有日期目录
// 返回: 文件ID列表
func (m *Manager) List(dateDir string) ([]string, error) {
	ctx := context.Background()

	var querySQL string
	var args []interface{}

	if dateDir == "" {
		querySQL = `SELECT id FROM attachments ORDER BY created_at DESC`
	} else {
		querySQL = `SELECT id FROM attachments WHERE date_dir = ? ORDER BY created_at DESC`
		args = []interface{}{dateDir}
	}

	rows, err := m.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("查询数据库失败: %w", err)
	}
	defer rows.Close()

	var fileIDs []string
	for rows.Next() {
		var fileID string
		if err := rows.Scan(&fileID); err != nil {
			return nil, fmt.Errorf("扫描结果失败: %w", err)
		}
		fileIDs = append(fileIDs, fileID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果失败: %w", err)
	}

	return fileIDs, nil
}

// ListAll 列出所有文件信息（从数据库读取）
// dateDir: 日期目录（格式：YYYY-MM-DD），如果为空则列出所有日期目录
// 返回: 文件信息列表
func (m *Manager) ListAll(dateDir string) ([]*FileInfo, error) {
	ctx := context.Background()

	var querySQL string
	var args []interface{}

	if dateDir == "" {
		querySQL = `
			SELECT id, filename, size, date_dir, relative_path, absolute_path,
			       mime_type, metadata, created_at, updated_at
			FROM attachments
			ORDER BY created_at DESC
		`
	} else {
		querySQL = `
			SELECT id, filename, size, date_dir, relative_path, absolute_path,
			       mime_type, metadata, created_at, updated_at
			FROM attachments
			WHERE date_dir = ?
			ORDER BY created_at DESC
		`
		args = []interface{}{dateDir}
	}

	rows, err := m.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("查询数据库失败: %w", err)
	}
	defer rows.Close()

	var files []*FileInfo
	for rows.Next() {
		var info FileInfo
		var mimeType sql.NullString
		var metadataJSON sql.NullString
		var createdAt, updatedAt time.Time

		if err := rows.Scan(
			&info.ID, &info.Name, &info.Size, &info.DateDir, &info.RelativePath,
			&info.AbsolutePath, &mimeType, &metadataJSON, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描结果失败: %w", err)
		}

		info.ModTime = updatedAt
		info.CreatedAt = createdAt
		info.UpdatedAt = updatedAt
		if mimeType.Valid {
			info.MimeType = mimeType.String
		}
		if metadataJSON.Valid && metadataJSON.String != "" {
			if err := json.Unmarshal([]byte(metadataJSON.String), &info.Metadata); err != nil {
				info.Metadata = nil
			}
		}

		files = append(files, &info)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果失败: %w", err)
	}

	return files, nil
}

// GetBaseDir 获取attachments基础目录的绝对路径
func (m *Manager) GetBaseDir() string {
	return m.baseDir
}
