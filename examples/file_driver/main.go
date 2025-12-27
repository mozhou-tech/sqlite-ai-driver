package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/file-driver" // 导入以注册 file 驱动
)

// Product 产品模型
type Product struct {
	ID          int
	Name        string
	Price       float64
	Description string
	CreatedAt   time.Time
}

func main() {
	// 获取当前工作目录
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("获取工作目录失败: %v", err)
	}

	// 数据库文件路径（支持多种格式）
	// 方式 1: 使用相对路径（会自动构建到 {DATA_DIR}/files/ 目录）
	// dbPath := "example.db"

	// 方式 2: 使用绝对路径
	dbPath := filepath.Join(wd, "testdata", "example.db")

	// 方式 3: 使用 file:// 协议
	// dbPath := "file://" + filepath.Join(wd, "example.db")

	fmt.Printf("📂 数据库路径: %s\n", dbPath)

	// 打开数据库连接（使用 file 驱动）
	db, err := sql.Open("file", dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 测试连接
	if err := db.Ping(); err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	fmt.Println("✅ 成功连接到数据库")

	// 设置连接池参数
	db.SetMaxIdleConns(10)
	db.SetMaxOpenConns(100)
	db.SetConnMaxLifetime(time.Hour)

	ctx := context.Background()

	// 创建表
	fmt.Println("\n📝 创建表...")
	if err := createTable(ctx, db); err != nil {
		log.Fatalf("创建表失败: %v", err)
	}
	fmt.Println("✅ 表创建成功")

	// 清空表数据（用于示例演示）
	fmt.Println("\n🗑️  清空表数据...")
	if _, err := db.ExecContext(ctx, "DELETE FROM products"); err != nil {
		log.Printf("警告: 清空表数据失败: %v", err)
	}

	// 示例：插入单条数据（写操作）
	fmt.Println("\n📝 插入单条数据...")
	productID, err := insertProduct(ctx, db, Product{
		Name:        "MacBook Pro",
		Price:       12999.00,
		Description: "Apple M3 芯片，14 英寸",
	})
	if err != nil {
		log.Fatalf("插入数据失败: %v", err)
	}
	fmt.Printf("✅ 插入成功，产品 ID: %d\n", productID)

	// 示例：批量插入数据（写操作）
	fmt.Println("\n📝 批量插入数据...")
	products := []Product{
		{Name: "iPhone 15", Price: 5999.00, Description: "128GB 存储"},
		{Name: "iPad Air", Price: 4399.00, Description: "M2 芯片，10.9 英寸"},
		{Name: "AirPods Pro", Price: 1899.00, Description: "主动降噪"},
	}
	insertedCount, err := insertProducts(ctx, db, products)
	if err != nil {
		log.Fatalf("批量插入失败: %v", err)
	}
	fmt.Printf("✅ 批量插入成功，共插入 %d 条数据\n", insertedCount)

	// 示例：查询单条数据（读操作）
	fmt.Println("\n🔍 查询单条数据...")
	product, err := getProductByID(ctx, db, productID)
	if err != nil {
		log.Fatalf("查询数据失败: %v", err)
	}
	fmt.Printf("✅ 查询成功:\n")
	printProduct(product)

	// 示例：查询所有数据（读操作）
	fmt.Println("\n🔍 查询所有数据...")
	allProducts, err := getAllProducts(ctx, db)
	if err != nil {
		log.Fatalf("查询所有数据失败: %v", err)
	}
	fmt.Printf("✅ 查询成功，共 %d 条数据:\n", len(allProducts))
	for _, p := range allProducts {
		printProduct(p)
	}

	// 示例：条件查询（读操作）
	fmt.Println("\n🔍 查询价格大于 5000 的产品...")
	expensiveProducts, err := getProductsByPrice(ctx, db, 5000.0)
	if err != nil {
		log.Fatalf("条件查询失败: %v", err)
	}
	fmt.Printf("✅ 查询成功，共 %d 条数据:\n", len(expensiveProducts))
	for _, p := range expensiveProducts {
		printProduct(p)
	}

	// 示例：更新数据（写操作）
	fmt.Println("\n✏️  更新数据...")
	updatedRows, err := updateProductPrice(ctx, db, productID, 11999.00)
	if err != nil {
		log.Fatalf("更新数据失败: %v", err)
	}
	fmt.Printf("✅ 更新成功，影响行数: %d\n", updatedRows)

	// 验证更新
	updatedProduct, err := getProductByID(ctx, db, productID)
	if err == nil {
		fmt.Printf("   更新后的价格: ¥%.2f\n", updatedProduct.Price)
	}

	// 示例：删除数据（写操作）
	fmt.Println("\n🗑️  删除数据...")
	// 先获取要删除的产品 ID（删除最后一个）
	if len(allProducts) > 0 {
		deleteID := allProducts[len(allProducts)-1].ID
		deletedRows, err := deleteProduct(ctx, db, deleteID)
		if err != nil {
			log.Fatalf("删除数据失败: %v", err)
		}
		fmt.Printf("✅ 删除成功，影响行数: %d\n", deletedRows)

		// 验证删除
		_, err = getProductByID(ctx, db, deleteID)
		if err == sql.ErrNoRows {
			fmt.Println("✅ 数据已成功删除")
		} else if err != nil {
			fmt.Printf("⚠️  验证删除时出错: %v\n", err)
		}
	}

	// 示例：统计数量（读操作）
	fmt.Println("\n📊 统计产品数量...")
	count, err := countProducts(ctx, db)
	if err != nil {
		log.Fatalf("统计失败: %v", err)
	}
	fmt.Printf("✅ 当前共有 %d 个产品\n", count)

	// 示例：事务操作（写操作）
	fmt.Println("\n💼 事务操作...")
	if err := transactionExample(ctx, db); err != nil {
		log.Fatalf("事务操作失败: %v", err)
	}
	fmt.Println("✅ 事务操作成功")

	// 最终统计
	fmt.Println("\n📊 最终统计...")
	finalCount, err := countProducts(ctx, db)
	if err != nil {
		log.Fatalf("统计失败: %v", err)
	}
	fmt.Printf("✅ 最终共有 %d 个产品\n", finalCount)

	fmt.Println("\n🎉 所有操作完成！")
}

// createTable 创建产品表
func createTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS products (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		price REAL NOT NULL,
		description TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	_, err := db.ExecContext(ctx, query)
	return err
}

// insertProduct 插入单个产品
func insertProduct(ctx context.Context, db *sql.DB, product Product) (int, error) {
	query := `INSERT INTO products (name, price, description) VALUES (?, ?, ?)`
	result, err := db.ExecContext(ctx, query, product.Name, product.Price, product.Description)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

// insertProducts 批量插入产品
func insertProducts(ctx context.Context, db *sql.DB, products []Product) (int, error) {
	query := `INSERT INTO products (name, price, description) VALUES (?, ?, ?)`

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for _, product := range products {
		_, err := stmt.ExecContext(ctx, product.Name, product.Price, product.Description)
		if err != nil {
			return 0, err
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return count, nil
}

// getProductByID 根据 ID 查询产品
func getProductByID(ctx context.Context, db *sql.DB, id int) (Product, error) {
	query := `SELECT id, name, price, description, created_at FROM products WHERE id = ?`
	var product Product
	err := db.QueryRowContext(ctx, query, id).Scan(
		&product.ID,
		&product.Name,
		&product.Price,
		&product.Description,
		&product.CreatedAt,
	)
	return product, err
}

// getAllProducts 查询所有产品
func getAllProducts(ctx context.Context, db *sql.DB) ([]Product, error) {
	query := `SELECT id, name, price, description, created_at FROM products ORDER BY id`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var product Product
		if err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Price,
			&product.Description,
			&product.CreatedAt,
		); err != nil {
			return nil, err
		}
		products = append(products, product)
	}
	return products, rows.Err()
}

// getProductsByPrice 根据价格查询产品
func getProductsByPrice(ctx context.Context, db *sql.DB, minPrice float64) ([]Product, error) {
	query := `SELECT id, name, price, description, created_at FROM products WHERE price > ? ORDER BY price DESC`
	rows, err := db.QueryContext(ctx, query, minPrice)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var product Product
		if err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Price,
			&product.Description,
			&product.CreatedAt,
		); err != nil {
			return nil, err
		}
		products = append(products, product)
	}
	return products, rows.Err()
}

// updateProductPrice 更新产品价格
func updateProductPrice(ctx context.Context, db *sql.DB, id int, newPrice float64) (int64, error) {
	query := `UPDATE products SET price = ? WHERE id = ?`
	result, err := db.ExecContext(ctx, query, newPrice, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// deleteProduct 删除产品
func deleteProduct(ctx context.Context, db *sql.DB, id int) (int64, error) {
	query := `DELETE FROM products WHERE id = ?`
	result, err := db.ExecContext(ctx, query, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// countProducts 统计产品数量
func countProducts(ctx context.Context, db *sql.DB) (int, error) {
	query := `SELECT COUNT(*) FROM products`
	var count int
	err := db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

// transactionExample 事务操作示例
func transactionExample(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 在事务中插入产品
	_, err = tx.ExecContext(ctx,
		`INSERT INTO products (name, price, description) VALUES (?, ?, ?)`,
		"Apple Watch", 2999.00, "Series 9, GPS + 蜂窝网络",
	)
	if err != nil {
		return err
	}

	// 在事务中更新产品
	_, err = tx.ExecContext(ctx,
		`UPDATE products SET price = price * 0.9 WHERE name = ?`,
		"Apple Watch",
	)
	if err != nil {
		return err
	}

	// 提交事务
	return tx.Commit()
}

// printProduct 打印产品信息
func printProduct(p Product) {
	fmt.Printf("  - ID: %d, 名称: %s, 价格: ¥%.2f, 描述: %s, 创建时间: %s\n",
		p.ID, p.Name, p.Price, p.Description, p.CreatedAt.Format("2006-01-02 15:04:05"))
}
