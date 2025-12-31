package attachments_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/attachments"
)

func ExampleManager() {
	// 创建临时目录作为工作目录
	tmpDir, _ := os.MkdirTemp("", "attachments_example")
	defer os.RemoveAll(tmpDir)

	// 创建附件管理器
	mgr, err := attachments.New(tmpDir)
	if err != nil {
		fmt.Printf("创建管理器失败: %v\n", err)
		return
	}

	// 存储文件
	filename := "example.txt"
	data := []byte("Hello, World!")
	fileID, err := mgr.Store(filename, data)
	if err != nil {
		fmt.Printf("存储文件失败: %v\n", err)
		return
	}
	fmt.Printf("文件已存储，ID: %s\n", fileID)

	// 获取文件信息
	info, err := mgr.GetInfo(fileID)
	if err != nil {
		fmt.Printf("获取文件信息失败: %v\n", err)
		return
	}
	fmt.Printf("文件名: %s, 大小: %d 字节\n", info.Name, info.Size)

	// 获取绝对路径
	absPath, err := mgr.GetAbsolutePath(fileID)
	if err != nil {
		fmt.Printf("获取绝对路径失败: %v\n", err)
		return
	}
	fmt.Printf("绝对路径: %s\n", absPath)

	// 读取文件
	readData, err := mgr.Read(fileID)
	if err != nil {
		fmt.Printf("读取文件失败: %v\n", err)
		return
	}
	fmt.Printf("文件内容: %s\n", string(readData))

	// 列出所有文件
	allFiles, err := mgr.List("")
	if err != nil {
		fmt.Printf("列出文件失败: %v\n", err)
		return
	}
	fmt.Printf("文件总数: %d\n", len(allFiles))

	// 删除文件
	if err := mgr.Delete(fileID); err != nil {
		fmt.Printf("删除文件失败: %v\n", err)
		return
	}
	fmt.Println("文件已删除")
}

func ExampleManager_StoreFromReader() {
	// 创建临时目录作为工作目录
	tmpDir, _ := os.MkdirTemp("", "attachments_example")
	defer os.RemoveAll(tmpDir)

	// 创建附件管理器
	mgr, err := attachments.New(tmpDir)
	if err != nil {
		fmt.Printf("创建管理器失败: %v\n", err)
		return
	}

	// 从文件读取并存储
	sourceFile := filepath.Join(tmpDir, "source.txt")
	os.WriteFile(sourceFile, []byte("Source file content"), 0644)

	file, _ := os.Open(sourceFile)
	defer file.Close()

	fileID, err := mgr.StoreFromReader("stored.txt", file)
	if err != nil {
		fmt.Printf("从Reader存储失败: %v\n", err)
		return
	}
	fmt.Printf("文件已存储，ID: %s\n", fileID)
}
