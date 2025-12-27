package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	// 导入 file-driver 以注册驱动
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/file-driver"
)

func main() {
	// 示例 1: 使用本地文件系统
	exampleLocalFile()

	// 示例 2: 使用 S3 存储（需要配置 AWS 凭证）
	// exampleS3File()

	// 示例 3: 使用 Google Cloud Storage（需要配置 GCS 凭证）
	// exampleGCSFile()
}

// exampleLocalFile 演示如何使用本地文件
func exampleLocalFile() {
	fmt.Println("=== 示例 1: 使用本地文件系统 ===")

	// 方式 1: 直接使用文件路径（推荐）
	dbPath := "testdata/example.db"
	db, err := sql.Open("file", dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 方式 2: 使用 file:// 协议
	// db, err := sql.Open("file", "file:///"+dbPath)

	// 测试连接
	if err := db.Ping(); err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	fmt.Printf("成功连接到本地数据库: %s\n", dbPath)

	// 创建表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT UNIQUE
		)
	`)
	if err != nil {
		log.Fatalf("创建表失败: %v", err)
	}

	// 插入数据
	result, err := db.Exec(`
		INSERT INTO users (name, email) VALUES (?, ?)
	`, "张三", "zhangsan@example.com")
	if err != nil {
		log.Fatalf("插入数据失败: %v", err)
	}

	id, _ := result.LastInsertId()
	fmt.Printf("插入数据成功，ID: %d\n", id)

	// 查询数据
	rows, err := db.Query("SELECT id, name, email FROM users")
	if err != nil {
		log.Fatalf("查询数据失败: %v", err)
	}
	defer rows.Close()

	fmt.Println("用户列表:")
	for rows.Next() {
		var id int
		var name, email string
		if err := rows.Scan(&id, &name, &email); err != nil {
			log.Fatalf("扫描数据失败: %v", err)
		}
		fmt.Printf("  ID: %d, 姓名: %s, 邮箱: %s\n", id, name, email)
	}

	// 清理测试文件
	os.Remove(dbPath)
	fmt.Println("示例完成\n")
}

// exampleS3File 演示如何使用 S3 存储的文件
// 注意：需要配置 AWS 凭证（环境变量或 ~/.aws/credentials）
func exampleS3File() {
	fmt.Println("=== 示例 2: 使用 S3 存储 ===")

	// S3 路径格式: s3://bucket-name/path/to/file.db
	s3Path := "s3://my-bucket/databases/example.db"

	db, err := sql.Open("file", s3Path)
	if err != nil {
		log.Fatalf("打开 S3 数据库失败: %v", err)
	}
	defer db.Close()

	// 测试连接
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("连接 S3 数据库失败: %v", err)
	}
	fmt.Printf("成功连接到 S3 数据库: %s\n", s3Path)

	// 注意：S3 文件是只读的，因为文件会被下载到临时文件
	// 如果需要写入，需要手动上传回 S3

	fmt.Println("示例完成\n")
}

// exampleGCSFile 演示如何使用 Google Cloud Storage 的文件
// 注意：需要配置 GCS 凭证（环境变量 GOOGLE_APPLICATION_CREDENTIALS）
func exampleGCSFile() {
	fmt.Println("=== 示例 3: 使用 Google Cloud Storage ===")

	// GCS 路径格式: gs://bucket-name/path/to/file.db
	gcsPath := "gs://my-bucket/databases/example.db"

	db, err := sql.Open("file", gcsPath)
	if err != nil {
		log.Fatalf("打开 GCS 数据库失败: %v", err)
	}
	defer db.Close()

	// 测试连接
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("连接 GCS 数据库失败: %v", err)
	}
	fmt.Printf("成功连接到 GCS 数据库: %s\n", gcsPath)

	// 注意：GCS 文件是只读的，因为文件会被下载到临时文件
	// 如果需要写入，需要手动上传回 GCS

	fmt.Println("示例完成\n")
}
