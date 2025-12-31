package attachments

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// getTestDataDir 获取测试数据目录路径
// 为每个测试创建独立的临时目录，避免数据库锁竞争
func getTestDataDir(t *testing.T) string {
	// 创建临时目录，测试结束后自动清理
	tmpDir := filepath.Join("./testdata", "attachments_test_"+t.Name())

	// 清理可能存在的旧目录
	os.RemoveAll(tmpDir)

	// 确保目录存在
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatalf("创建测试目录失败: %v", err)
	}

	// 测试结束后清理
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	return tmpDir
}

func TestNew(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)
	fmt.Printf("workingDir: %s\n", testDataDir)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 检查attachments目录是否创建
	attachmentsDir := filepath.Join(testDataDir, "attachments")
	if _, err := os.Stat(attachmentsDir); os.IsNotExist(err) {
		t.Errorf("attachments目录未创建")
	}

	// 检查baseDir（需要转换为绝对路径进行比较，因为GetBaseDir返回绝对路径）
	absAttachmentsDir, err := filepath.Abs(attachmentsDir)
	if err != nil {
		t.Fatalf("获取绝对路径失败: %v", err)
	}
	if mgr.GetBaseDir() != absAttachmentsDir {
		t.Errorf("baseDir不匹配: 期望 %s, 实际 %s", absAttachmentsDir, mgr.GetBaseDir())
	}

	// 不再检查数据库文件，因为已移除数据库功能
}

func TestStore(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储文件
	filename := "test.txt"
	data := []byte("Hello, World!")

	// 清理可能存在的旧文件，避免文件重命名导致测试失败
	expectedDateDir := time.Now().Format("2006-01-02")
	expectedFileID := filepath.Join(expectedDateDir, filename)
	expectedFilePath := filepath.Join(mgr.GetBaseDir(), expectedFileID)
	// 直接删除文件系统中的文件（如果存在）
	if err := os.Remove(expectedFilePath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("清理旧文件失败: %v", err)
	}
	// 同时尝试从数据库删除（如果存在）
	_ = mgr.Delete(expectedFileID)

	fileID, err := mgr.Store(filename, data)
	if err != nil {
		t.Fatalf("存储文件失败: %v", err)
	}

	// 检查文件ID格式（应该是日期目录/文件名）
	if fileID != expectedFileID {
		t.Errorf("文件ID不匹配: 期望 %s, 实际 %s", expectedFileID, fileID)
	}

	// 检查文件是否存在
	absPath, err := mgr.GetAbsolutePath(fileID)
	if err != nil {
		t.Fatalf("获取绝对路径失败: %v", err)
	}

	// 读取文件内容
	readData, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}

	if string(readData) != string(data) {
		t.Errorf("文件内容不匹配: 期望 %s, 实际 %s", string(data), string(readData))
	}
}

func TestDelete(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储文件
	filename := "test_delete.txt"
	data := []byte("Test delete")
	fileID, err := mgr.Store(filename, data)
	if err != nil {
		t.Fatalf("存储文件失败: %v", err)
	}

	// 删除文件
	if err := mgr.Delete(fileID); err != nil {
		t.Fatalf("删除文件失败: %v", err)
	}

	// 验证文件已删除
	_, err = mgr.GetAbsolutePath(fileID)
	if err == nil {
		t.Errorf("文件应该已被删除")
	}
}

func TestGetInfo(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储文件
	filename := "test_info.txt"
	data := []byte("Test info")
	fileID, err := mgr.Store(filename, data)
	if err != nil {
		t.Fatalf("存储文件失败: %v", err)
	}

	// 获取文件信息
	info, err := mgr.GetInfo(fileID)
	if err != nil {
		t.Fatalf("获取文件信息失败: %v", err)
	}

	// 验证信息
	if info.Name != filename {
		t.Errorf("文件名不匹配: 期望 %s, 实际 %s", filename, info.Name)
	}

	if info.Size != int64(len(data)) {
		t.Errorf("文件大小不匹配: 期望 %d, 实际 %d", len(data), info.Size)
	}

	if info.RelativePath != fileID {
		t.Errorf("相对路径不匹配: 期望 %s, 实际 %s", fileID, info.RelativePath)
	}

	expectedDateDir := time.Now().Format("2006-01-02")
	if info.DateDir != expectedDateDir {
		t.Errorf("日期目录不匹配: 期望 %s, 实际 %s", expectedDateDir, info.DateDir)
	}
}

func TestGetAbsolutePath(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储文件
	filename := "test_path.txt"
	data := []byte("Test path")
	fileID, err := mgr.Store(filename, data)
	if err != nil {
		t.Fatalf("存储文件失败: %v", err)
	}

	// 获取绝对路径
	absPath, err := mgr.GetAbsolutePath(fileID)
	if err != nil {
		t.Fatalf("获取绝对路径失败: %v", err)
	}

	// 验证路径是绝对的
	if !filepath.IsAbs(absPath) {
		t.Errorf("路径不是绝对路径: %s", absPath)
	}

	// 验证文件存在
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Errorf("文件不存在: %s", absPath)
	}
}

func TestRead(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储文件
	filename := "test_read.txt"
	data := []byte("Test read content")
	fileID, err := mgr.Store(filename, data)
	if err != nil {
		t.Fatalf("存储文件失败: %v", err)
	}

	// 读取文件
	readData, err := mgr.Read(fileID)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}

	// 验证内容
	if string(readData) != string(data) {
		t.Errorf("文件内容不匹配: 期望 %s, 实际 %s", string(data), string(readData))
	}
}

func TestList(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储多个文件
	files := []string{"file1.txt", "file2.txt", "file3.txt"}
	var fileIDs []string
	for _, filename := range files {
		data := []byte("Content: " + filename)
		fileID, err := mgr.Store(filename, data)
		if err != nil {
			t.Fatalf("存储文件失败: %v", err)
		}
		fileIDs = append(fileIDs, fileID)
	}

	// 列出所有文件
	allFiles, err := mgr.List("")
	if err != nil {
		t.Fatalf("列出文件失败: %v", err)
	}

	// 验证文件数量
	if len(allFiles) < len(files) {
		t.Errorf("文件数量不匹配: 期望至少 %d, 实际 %d", len(files), len(allFiles))
	}

	// 验证文件ID都在列表中
	fileMap := make(map[string]bool)
	for _, f := range allFiles {
		fileMap[f] = true
	}

	for _, fileID := range fileIDs {
		if !fileMap[fileID] {
			t.Errorf("文件ID不在列表中: %s", fileID)
		}
	}
}

func TestListByDate(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储文件
	filename := "test_date.txt"
	data := []byte("Test date")
	fileID, err := mgr.Store(filename, data)
	if err != nil {
		t.Fatalf("存储文件失败: %v", err)
	}

	// 获取日期目录
	dateDir := time.Now().Format("2006-01-02")

	// 列出指定日期的文件
	files, err := mgr.List(dateDir)
	if err != nil {
		t.Fatalf("列出文件失败: %v", err)
	}

	// 验证文件在列表中
	found := false
	for _, f := range files {
		if f == fileID {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("文件不在指定日期的列表中: %s", fileID)
	}
}

func TestStoreWithMetadata(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储文件并添加元数据
	filename := "test_metadata.txt"
	data := []byte("Test with metadata")
	mimeType := "text/plain"
	metadata := map[string]interface{}{
		"author": "test",
		"tags":   []string{"test", "metadata"},
	}

	fileID, err := mgr.StoreWithMetadata(filename, data, &mimeType, metadata)
	if err != nil {
		t.Fatalf("存储文件失败: %v", err)
	}

	// 获取文件信息
	info, err := mgr.GetInfo(fileID)
	if err != nil {
		t.Fatalf("获取文件信息失败: %v", err)
	}

	// 验证文件基本信息（元数据不再存储）
	if info.Name != filename {
		t.Errorf("文件名不匹配: 期望 %s, 实际 %s", filename, info.Name)
	}

	if info.Size != int64(len(data)) {
		t.Errorf("文件大小不匹配: 期望 %d, 实际 %d", len(data), info.Size)
	}

	// 验证元数据字段为空（因为不再存储元数据）
	if info.MimeType != "" {
		t.Errorf("MIME类型应该为空，实际为: %s", info.MimeType)
	}

	if info.Metadata != nil {
		t.Errorf("元数据应该为空，实际为: %v", info.Metadata)
	}
}

func TestListAll(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储多个文件
	files := []string{"file1.txt", "file2.txt", "file3.txt"}
	var fileIDs []string
	for _, filename := range files {
		data := []byte("Content: " + filename)
		fileID, err := mgr.Store(filename, data)
		if err != nil {
			t.Fatalf("存储文件失败: %v", err)
		}
		fileIDs = append(fileIDs, fileID)
	}

	// 列出所有文件信息
	allFiles, err := mgr.ListAll("")
	if err != nil {
		t.Fatalf("列出文件失败: %v", err)
	}

	// 验证文件数量
	if len(allFiles) < len(files) {
		t.Errorf("文件数量不匹配: 期望至少 %d, 实际 %d", len(files), len(allFiles))
	}

	// 验证文件ID都在列表中
	fileMap := make(map[string]bool)
	for _, f := range allFiles {
		fileMap[f.ID] = true
	}

	for _, fileID := range fileIDs {
		if !fileMap[fileID] {
			t.Errorf("文件ID不在列表中: %s", fileID)
		}
	}
}

func TestGetInfoFromDatabase(t *testing.T) {
	// 使用 testdata 目录
	testDataDir := getTestDataDir(t)

	// 创建管理器
	mgr, err := New(testDataDir)
	if err != nil {
		t.Fatalf("创建管理器失败: %v", err)
	}
	defer mgr.Close()

	// 存储文件
	filename := "test_db_info.txt"
	data := []byte("Test database info")
	fileID, err := mgr.Store(filename, data)
	if err != nil {
		t.Fatalf("存储文件失败: %v", err)
	}

	// 获取文件信息（应该从数据库读取）
	info, err := mgr.GetInfo(fileID)
	if err != nil {
		t.Fatalf("获取文件信息失败: %v", err)
	}

	// 验证信息来自数据库
	if info.ID != fileID {
		t.Errorf("文件ID不匹配: 期望 %s, 实际 %s", fileID, info.ID)
	}

	if info.Name != filename {
		t.Errorf("文件名不匹配: 期望 %s, 实际 %s", filename, info.Name)
	}

	// 验证创建时间和更新时间
	if info.CreatedAt.IsZero() {
		t.Errorf("创建时间为空")
	}

	if info.UpdatedAt.IsZero() {
		t.Errorf("更新时间为空")
	}
}

// testReader 用于测试的Reader实现
type testReader struct {
	data []byte
	pos  int
}

func (r *testReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
