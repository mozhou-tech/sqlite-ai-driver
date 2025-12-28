package duckdb_driver

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
)

// extractSearchTerms 从查询中提取关键词
// 对于英文文本，提取长度 >= 3 的单词（过滤掉常见的停用词）
// 对于中文文本，使用分词结果
func extractSearchTerms(query, queryTokens string) []string {
	var terms []string

	// 如果分词结果不为空且与原始查询不同，使用分词结果
	if queryTokens != "" && queryTokens != query {
		// 分词结果是用空格分隔的
		tokenList := strings.Fields(queryTokens)
		for _, token := range tokenList {
			token = strings.TrimSpace(token)
			if len(token) >= 2 { // 中文词至少2个字符
				terms = append(terms, token)
			}
		}
		if len(terms) > 0 {
			return terms
		}
	}

	// 对于英文文本，提取单词
	// 简单的英文单词提取（匹配字母和数字）
	wordRegex := regexp.MustCompile(`[a-zA-Z0-9]+`)
	words := wordRegex.FindAllString(query, -1)

	// 常见的英文停用词
	stopWords := map[string]bool{
		"the": true, "is": true, "are": true, "was": true, "were": true,
		"a": true, "an": true, "and": true, "or": true, "but": true,
		"in": true, "on": true, "at": true, "to": true, "for": true,
		"of": true, "with": true, "by": true, "from": true, "as": true,
		"what": true, "where": true, "when": true, "who": true, "why": true,
		"how": true, "this": true, "that": true, "these": true, "those": true,
	}

	for _, word := range words {
		word = strings.ToLower(word)
		// 只保留长度 >= 3 且不是停用词的单词
		if len(word) >= 3 && !stopWords[word] {
			terms = append(terms, word)
		}
	}

	return terms
}

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
	useFallback := false
	if err != nil {
		// 如果 FTS 查询失败，使用 LIKE 查询作为回退
		useFallback = true
	} else {
		// 检查是否有结果
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan row: %w", err)
			}
			ids = append(ids, id)
		}
		rows.Close()

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

		// 如果有结果，直接返回
		if len(ids) > 0 {
			return ids, nil
		}

		// 如果没有结果，尝试回退到 LIKE 查询
		useFallback = true
	}

	// 使用 LIKE 查询作为回退
	if useFallback {
		// 提取查询中的关键词（简单的英文单词提取）
		searchTerms := extractSearchTerms(query, queryTokens)

		if len(searchTerms) == 0 {
			// 如果没有提取到关键词，使用原始查询
			searchTerms = []string{query}
		}

		// 构建 LIKE 查询，同时在 content 和 content_tokens 列上搜索
		var conditions []string
		var args []interface{}

		for _, term := range searchTerms {
			// 在 content 列上搜索 (使用 ILIKE 进行不区分大小写的搜索)
			conditions = append(conditions, fmt.Sprintf("%s ILIKE ?", contentColumn))
			args = append(args, "%"+term+"%")
			// 如果 tokensColumn 存在，也在 tokensColumn 上搜索
			if count > 0 {
				conditions = append(conditions, fmt.Sprintf("%s ILIKE ?", tokensColumn))
				args = append(args, "%"+term+"%")
			}
		}

		searchSQL = fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE %s
			LIMIT ?
		`, idColumn, tableName, strings.Join(conditions, " OR "))
		args = append(args, limit)

		rows, err = db.QueryContext(ctx, searchSQL, args...)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}
		defer rows.Close()

		var ids []string
		idMap := make(map[string]bool) // 用于去重
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, fmt.Errorf("failed to scan row: %w", err)
			}
			if !idMap[id] {
				ids = append(ids, id)
				idMap[id] = true
			}
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

	// 这不应该到达，但为了安全起见
	return []string{}, nil
}
