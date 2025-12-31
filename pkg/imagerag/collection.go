package imagerag

import (
	"context"
	"database/sql"
	"fmt"
)

// Collection 集合
type Collection struct {
	db        *sql.DB
	tableName string
}

// createCollection 创建集合（使用共享表，每行数据都有 text_embedding 和 image_embedding 字段）
func (r *ImageRAG) createCollection(ctx context.Context, name string) (*Collection, error) {
	// 使用统一的表名
	tableName := "imagerag_documents"

	// 创建表（如果不存在）
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR PRIMARY KEY,
			content TEXT,
			metadata JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			_rev INTEGER DEFAULT 1
		)
	`, tableName)

	_, err := r.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	// 检查并创建必要的列
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM information_schema.columns 
		WHERE table_name = '%s' AND column_name = ?
	`, tableName)

	var count int

	// 创建 text_embedding 列
	err = r.db.QueryRowContext(ctx, checkColumnSQL, "text_embedding").Scan(&count)
	if err == nil && count == 0 {
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN text_embedding FLOAT[]`, tableName)
		_, _ = r.db.ExecContext(ctx, alterTableSQL)
	}

	// 创建 image_embedding 列
	err = r.db.QueryRowContext(ctx, checkColumnSQL, "image_embedding").Scan(&count)
	if err == nil && count == 0 {
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN image_embedding FLOAT[]`, tableName)
		_, _ = r.db.ExecContext(ctx, alterTableSQL)
	}

	// 创建 embedding_status 列
	statusColumn := "embedding_status"
	err = r.db.QueryRowContext(ctx, checkColumnSQL, statusColumn).Scan(&count)
	if err == nil && count == 0 {
		alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s VARCHAR DEFAULT 'pending'`, tableName, statusColumn)
		_, _ = r.db.ExecContext(ctx, alterTableSQL)
	}

	return &Collection{
		db:        r.db,
		tableName: tableName,
	}, nil
}
