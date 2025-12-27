package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/benbjohnson/litestream"
	"github.com/benbjohnson/litestream/file"
	_ "modernc.org/sqlite"
)

// Article 文章模型
type Article struct {
	ID        int
	Title     string
	Content   string
	Author    string
	CreatedAt time.Time
}

func main() {
	// 获取当前工作目录
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("获取工作目录失败: %v", err)
	}

	// 数据库文件路径
	dbPath := filepath.Join(wd, "testdata", "litestream_example.db")

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Fatalf("创建目录失败: %v", err)
	}

	fmt.Printf("📂 数据库路径: %s\n", dbPath)

	// 使用 modernc.org/sqlite 驱动打开数据库
	// 注意：litestream 需要 WAL 模式才能正常工作
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer sqlDB.Close()

	// 测试连接
	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	fmt.Println("✅ 成功连接到 SQLite 数据库")

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	ctx := context.Background()

	// 初始化 Litestream
	fmt.Println("\n💾 初始化 Litestream...")
	lsDB, cleanup, err := setupLitestream(ctx, dbPath)
	if err != nil {
		log.Printf("⚠️  Litestream 初始化失败: %v", err)
		log.Println("   提示: 将使用普通数据库操作，不进行备份")
		lsDB = nil
	} else {
		defer cleanup()
		fmt.Println("✅ Litestream 初始化成功，开始实时备份")
	}

	// 创建表
	fmt.Println("\n📝 创建表...")
	if err := createTable(ctx, sqlDB); err != nil {
		log.Fatalf("创建表失败: %v", err)
	}
	fmt.Println("✅ 表创建成功")

	// 清空表数据（用于示例演示）
	fmt.Println("\n🗑️  清空表数据...")
	if _, err := sqlDB.ExecContext(ctx, "DELETE FROM articles"); err != nil {
		log.Printf("警告: 清空表数据失败: %v", err)
	}

	// 示例：插入数据
	fmt.Println("\n📝 插入文章数据...")
	articles := []Article{
		{Title: "Litestream 简介", Content: "Litestream 是一个用于 SQLite 数据库的流式复制工具...", Author: "张三"},
		{Title: "SQLite WAL 模式", Content: "WAL (Write-Ahead Logging) 模式是 SQLite 的一种日志模式...", Author: "李四"},
		{Title: "数据库备份策略", Content: "定期备份是数据库管理的重要环节...", Author: "王五"},
	}

	for _, article := range articles {
		if err := insertArticle(ctx, sqlDB, article); err != nil {
			log.Fatalf("插入文章失败: %v", err)
		}
	}
	fmt.Printf("✅ 成功插入 %d 篇文章\n", len(articles))

	// 手动同步到备份（如果 Litestream 已启用）
	if lsDB != nil {
		fmt.Println("\n💾 同步数据到备份...")
		if err := lsDB.Sync(ctx); err != nil {
			log.Printf("⚠️  同步失败: %v", err)
		} else {
			fmt.Println("✅ 数据已同步到备份")
		}
	}

	// 查询所有文章
	fmt.Println("\n🔍 查询所有文章...")
	allArticles, err := getAllArticles(ctx, sqlDB)
	if err != nil {
		log.Fatalf("查询文章失败: %v", err)
	}
	fmt.Printf("✅ 查询成功，共 %d 篇文章:\n", len(allArticles))
	for _, a := range allArticles {
		printArticle(a)
	}

	// 演示 Litestream 快照功能
	if lsDB != nil {
		fmt.Println("\n📸 创建数据库快照...")
		if err := demonstrateLitestreamSnapshot(ctx, lsDB); err != nil {
			log.Printf("⚠️  快照创建失败: %v", err)
		}
	}

	// 继续演示数据库操作
	fmt.Println("\n📝 继续插入更多数据...")
	moreArticles := []Article{
		{Title: "Go 语言数据库驱动", Content: "Go 标准库提供了 database/sql 接口...", Author: "赵六"},
		{Title: "现代 SQLite 应用", Content: "SQLite 在现代应用开发中越来越受欢迎...", Author: "孙七"},
	}

	for _, article := range moreArticles {
		if err := insertArticle(ctx, sqlDB, article); err != nil {
			log.Fatalf("插入文章失败: %v", err)
		}
	}
	fmt.Printf("✅ 成功插入 %d 篇文章\n", len(moreArticles))

	// 再次同步到备份
	if lsDB != nil {
		fmt.Println("\n💾 再次同步数据到备份...")
		if err := lsDB.Sync(ctx); err != nil {
			log.Printf("⚠️  同步失败: %v", err)
		} else {
			fmt.Println("✅ 数据已同步到备份")
		}
	}

	// 最终统计
	fmt.Println("\n📊 最终统计...")
	finalCount, err := countArticles(ctx, sqlDB)
	if err != nil {
		log.Fatalf("统计失败: %v", err)
	}
	fmt.Printf("✅ 最终共有 %d 篇文章\n", finalCount)

	fmt.Println("\n🎉 所有操作完成！")
	fmt.Println("\n💡 提示:")
	fmt.Println("   - Litestream 需要 WAL 模式才能正常工作")
	fmt.Println("   - 备份文件存储在 testdata/backup 目录")
	fmt.Println("   - 可以使用 litestream restore 命令恢复数据库")
	fmt.Println("   - 更多信息请参考: https://litestream.io/")
}

// createTable 创建文章表
func createTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS articles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		author TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	_, err := db.ExecContext(ctx, query)
	return err
}

// insertArticle 插入文章
func insertArticle(ctx context.Context, db *sql.DB, article Article) error {
	query := `INSERT INTO articles (title, content, author) VALUES (?, ?, ?)`
	_, err := db.ExecContext(ctx, query, article.Title, article.Content, article.Author)
	return err
}

// getAllArticles 查询所有文章
func getAllArticles(ctx context.Context, db *sql.DB) ([]Article, error) {
	query := `SELECT id, title, content, author, created_at FROM articles ORDER BY id`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var articles []Article
	for rows.Next() {
		var article Article
		if err := rows.Scan(
			&article.ID,
			&article.Title,
			&article.Content,
			&article.Author,
			&article.CreatedAt,
		); err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	return articles, rows.Err()
}

// countArticles 统计文章数量
func countArticles(ctx context.Context, db *sql.DB) (int, error) {
	query := `SELECT COUNT(*) FROM articles`
	var count int
	err := db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

// printArticle 打印文章信息
func printArticle(a Article) {
	fmt.Printf("  - ID: %d, 标题: %s, 作者: %s, 创建时间: %s\n",
		a.ID, a.Title, a.Author, a.CreatedAt.Format("2006-01-02 15:04:05"))
}

// setupLitestream 初始化 Litestream 数据库和副本
// 返回 litestream.DB 实例和清理函数
func setupLitestream(ctx context.Context, dbPath string) (*litestream.DB, func(), error) {
	// 创建备份目录
	backupDir := filepath.Join(filepath.Dir(dbPath), "backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("创建备份目录失败: %w", err)
	}

	fmt.Printf("   📂 备份目录: %s\n", backupDir)

	// 创建 Litestream 数据库实例
	lsDB := litestream.NewDB(dbPath)
	lsDB.MonitorInterval = 1 * time.Second
	lsDB.CheckpointInterval = 1 * time.Minute
	lsDB.MinCheckpointPageN = 1000
	lsDB.MaxCheckpointPageN = 10000

	// 创建文件副本客户端（用于本地文件备份）
	fileClient := file.NewReplicaClient()
	fileClient.Path = backupDir

	// 创建副本并附加到数据库
	replica := litestream.NewReplica(lsDB)
	replica.Client = fileClient
	replica.SyncInterval = 1 * time.Second
	lsDB.Replica = replica

	// 打开数据库并开始复制
	if err := lsDB.Open(); err != nil {
		return nil, nil, fmt.Errorf("打开 Litestream 数据库失败: %w", err)
	}

	// 创建清理函数
	cleanup := func() {
		if err := lsDB.Close(ctx); err != nil {
			log.Printf("关闭 Litestream 数据库失败: %v", err)
		}
	}

	// 启动后台复制（在 goroutine 中）
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// 这里可以添加持续监控逻辑
		// 在实际应用中，你可能想要持续运行直到程序退出
	}()

	return lsDB, cleanup, nil
}

// demonstrateLitestreamSnapshot 演示创建数据库快照
func demonstrateLitestreamSnapshot(ctx context.Context, lsDB *litestream.DB) error {
	// 创建完整快照
	info, err := lsDB.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("快照创建失败: %w", err)
	}

	fmt.Printf("   ✅ 快照创建成功\n")
	fmt.Printf("   📊 事务 ID: %s\n", info.MaxTXID)
	fmt.Printf("   📦 大小: %d 字节\n", info.Size)

	// 列出备份文件
	backupDir := filepath.Join(filepath.Dir(lsDB.Path()), "backup")
	files, err := os.ReadDir(backupDir)
	if err == nil && len(files) > 0 {
		fmt.Println("   📋 备份文件列表:")
		for _, file := range files {
			info, err := file.Info()
			if err == nil {
				fmt.Printf("      - %s (大小: %d 字节)\n", file.Name(), info.Size())
			}
		}
	}

	return nil
}
