package duckdb_driver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
)

// TokenizeWithSego 使用 sego 对文本进行中文分词，返回用空格分隔的词
// 这是 duckdb-driver 包提供的公共 API，供外部使用
func TokenizeWithSego(text string) string {
	if text == "" {
		return ""
	}

	segmenter, err := sego.GetSegmenter()
	if err != nil {
		// 如果 sego 初始化失败，返回原文
		return text
	}

	segments := segmenter.Segment([]byte(text))
	var tokens []string
	for _, seg := range segments {
		token := seg.Token().Text()
		// 过滤掉空白字符和标点符号
		token = strings.TrimSpace(token)
		if token != "" && len(token) > 0 {
			tokens = append(tokens, token)
		}
	}

	if len(tokens) == 0 {
		return text // 如果分词结果为空，返回原文
	}

	return strings.Join(tokens, " ")
}

// CreateFTSIndexWithSego 创建支持 sego 中文分词的 DuckDB FTS 索引
// 参数：
//   - db: DuckDB 数据库连接
//   - tableName: 表名
//   - idColumn: ID 列名（用于 FTS 索引）
//   - contentColumn: 内容列名（原始文本）
//   - tokensColumn: 分词结果列名（可选，如果为空则自动使用 contentColumn + "_tokens"）
//
// 此函数会：
// 1. 检查 tokensColumn 是否存在，如果不存在则创建
// 2. 创建 FTS 索引，同时索引 contentColumn 和 tokensColumn
func CreateFTSIndexWithSego(ctx context.Context, db *sql.DB, tableName, idColumn, contentColumn, tokensColumn string) error {
	if tokensColumn == "" {
		tokensColumn = contentColumn + "_tokens"
	}

	// 检查 tokensColumn 是否存在，如果不存在则创建
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM pragma_table_info('%s') 
		WHERE name = ?
	`, tableName)

	var count int
	err := db.QueryRowContext(ctx, checkColumnSQL, tokensColumn).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check column existence: %w", err)
	}

	if count == 0 {
		// 创建 tokensColumn
		alterTableSQL := fmt.Sprintf(`
			ALTER TABLE %s ADD COLUMN %s TEXT
		`, tableName, tokensColumn)
		_, err = db.ExecContext(ctx, alterTableSQL)
		if err != nil {
			return fmt.Errorf("failed to add tokens column: %w", err)
		}
	}

	// 创建 FTS 索引，同时索引 contentColumn 和 tokensColumn
	// DuckDB 的 FTS 扩展使用 PRAGMA create_fts_index 创建索引
	createFTSSQL := fmt.Sprintf(`PRAGMA create_fts_index('%s', '%s', '%s', '%s');`,
		tableName, idColumn, contentColumn, tokensColumn)
	_, err = db.ExecContext(ctx, createFTSSQL)
	if err != nil {
		// 如果索引已存在，忽略错误
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate") {
			return nil
		}
		return fmt.Errorf("failed to create FTS index: %w", err)
	}

	return nil
}

// UpdateContentTokens 更新指定文档的 content_tokens 字段
// 参数：
//   - ctx: 上下文
//   - db: DuckDB 数据库连接
//   - tableName: 表名
//   - idColumn: ID 列名
//   - idValue: 文档 ID 值
//   - contentColumn: 内容列名
//   - tokensColumn: 分词结果列名（可选，如果为空则自动使用 contentColumn + "_tokens"）
func UpdateContentTokens(ctx context.Context, db *sql.DB, tableName, idColumn, idValue, contentColumn, tokensColumn string) error {
	if tokensColumn == "" {
		tokensColumn = contentColumn + "_tokens"
	}

	// 获取 content 值
	var content string
	selectSQL := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", contentColumn, tableName, idColumn)
	err := db.QueryRowContext(ctx, selectSQL, idValue).Scan(&content)
	if err != nil {
		return fmt.Errorf("failed to get content: %w", err)
	}

	// 使用 sego 分词
	tokens := TokenizeWithSego(content)

	// 更新 tokensColumn
	updateSQL := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", tableName, tokensColumn, idColumn)
	_, err = db.ExecContext(ctx, updateSQL, tokens, idValue)
	if err != nil {
		return fmt.Errorf("failed to update tokens: %w", err)
	}

	return nil
}

// SearchWithSego 使用 sego 分词进行全文搜索
// 参数：
//   - ctx: 上下文
//   - db: DuckDB 数据库连接
//   - tableName: 表名
//   - query: 搜索查询文本
//   - contentColumn: 内容列名
//   - tokensColumn: 分词结果列名（可选，如果为空则自动使用 contentColumn + "_tokens"）
//   - limit: 返回结果数量限制
//
// 返回：匹配的文档 ID 列表和错误
func SearchWithSego(ctx context.Context, db *sql.DB, tableName, query, contentColumn, tokensColumn string, limit int) ([]string, error) {
	if tokensColumn == "" {
		tokensColumn = contentColumn + "_tokens"
	}

	// 使用 sego 对查询进行分词
	queryTokens := TokenizeWithSego(query)

	// 检查 tokensColumn 是否存在
	checkColumnSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM pragma_table_info('%s') 
		WHERE name = ?
	`, tableName)

	var count int
	err := db.QueryRowContext(ctx, checkColumnSQL, tokensColumn).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to check column existence: %w", err)
	}

	var searchSQL string
	var searchText string
	var idColumn string

	// 获取 ID 列名（假设第一列是 ID）
	getIDColumnSQL := fmt.Sprintf(`
		SELECT name 
		FROM pragma_table_info('%s') 
		LIMIT 1
	`, tableName)
	err = db.QueryRowContext(ctx, getIDColumnSQL).Scan(&idColumn)
	if err != nil {
		return nil, fmt.Errorf("failed to get ID column: %w", err)
	}

	if count > 0 && queryTokens != "" {
		// 使用分词结果搜索 tokensColumn 字段
		searchSQL = fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE %s MATCH ?
			LIMIT ?
		`, idColumn, tableName, tokensColumn)
		searchText = queryTokens
	} else {
		// 回退到原始 content 字段搜索
		searchSQL = fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE %s MATCH ?
			LIMIT ?
		`, idColumn, tableName, contentColumn)
		searchText = query
	}

	rows, err := db.QueryContext(ctx, searchSQL, searchText, limit)
	if err != nil {
		// 如果 FTS 查询失败，使用 LIKE 查询作为回退
		if count > 0 && queryTokens != "" {
			searchSQL = fmt.Sprintf(`
				SELECT %s
				FROM %s
				WHERE %s LIKE ?
				LIMIT ?
			`, idColumn, tableName, tokensColumn)
			searchPattern := "%" + queryTokens + "%"
			rows, err = db.QueryContext(ctx, searchSQL, searchPattern, limit)
		} else {
			searchSQL = fmt.Sprintf(`
				SELECT %s
				FROM %s
				WHERE %s LIKE ?
				LIMIT ?
			`, idColumn, tableName, contentColumn)
			searchPattern := "%" + query + "%"
			rows, err = db.QueryContext(ctx, searchSQL, searchPattern, limit)
		}
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	// 如果没有结果，返回空切片而不是 nil
	if ids == nil {
		return []string{}, nil
	}

	return ids, nil
}
