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

// createCollection 创建集合（表）
func (r *ImageRAG) createCollection(ctx context.Context, name string) (*Collection, error) {
	tableName := name
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

	// 创建embedding_status列
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM information_schema.columns 
		WHERE table_name = '%s' AND column_name = ?
	`, tableName)

	var count int
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

