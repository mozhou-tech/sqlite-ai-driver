package imagesearch

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Collection 集合
type Collection struct {
	db        *gorm.DB
	tableName string
}

// createCollection 创建集合（使用共享表，每行数据都有 text_embedding 和 image_embedding 字段）
// 表名使用 tablePrefix 前缀，存储在 sqlite-driver 提供的共享数据库文件中
func (r *ImageSearch) createCollection(ctx context.Context, name string) (*Collection, error) {
	// 使用统一的表名，前缀为 tablePrefix
	tableName := r.tablePrefix + name

	// 使用 GORM AutoMigrate 创建表（如果不存在）
	// 注意：GORM 的 AutoMigrate 会自动处理列的增加，但不会删除列
	// 我们需要手动处理 text_embedding 和 image_embedding 列，因为它们是 TEXT 类型存储 JSON 数组
	documentModel := Document{}
	if err := r.db.WithContext(ctx).Table(tableName).AutoMigrate(&documentModel); err != nil {
		return nil, fmt.Errorf("failed to migrate table: %w", err)
	}

	// 检查并创建必要的列（如果不存在）
	// SQLite 不支持直接检查列是否存在，所以我们使用 ALTER TABLE IF NOT EXISTS 的变通方法
	// 由于 SQLite 的限制，我们直接尝试添加列，如果已存在会忽略错误

	// 创建 text_embedding 列（如果不存在）
	_ = r.db.WithContext(ctx).Exec(fmt.Sprintf(`
		ALTER TABLE %s ADD COLUMN text_embedding TEXT
	`, tableName)).Error

	// 创建 image_embedding 列（如果不存在）
	_ = r.db.WithContext(ctx).Exec(fmt.Sprintf(`
		ALTER TABLE %s ADD COLUMN image_embedding TEXT
	`, tableName)).Error

	// 创建 embedding_status 列（如果不存在）
	_ = r.db.WithContext(ctx).Exec(fmt.Sprintf(`
		ALTER TABLE %s ADD COLUMN embedding_status TEXT DEFAULT 'pending'
	`, tableName)).Error

	return &Collection{
		db:        r.db,
		tableName: tableName,
	}, nil
}
