package duckdb_driver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestDB 创建测试数据库并返回连接
func setupTestDB(t *testing.T, dbName string) (*sql.DB, string) {
	testdataDir := getProjectRootTestdata()
	dbPath := filepath.Join(testdataDir, dbName)

	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	return db, dbPath
}

// cleanupTestDB 清理测试数据库
func cleanupTestDB(t *testing.T, db *sql.DB, dbPath string) {
	if db != nil {
		db.Close()
	}
	if dbPath != "" {
		os.Remove(dbPath)
	}
}

func TestTokenizeWithSego(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // 期望结果包含的关键词（因为分词结果可能因词典而异）
		notEmpty bool   // 是否期望非空结果
	}{
		{
			name:     "空字符串",
			input:    "",
			expected: "",
			notEmpty: false,
		},
		{
			name:     "中文文本",
			input:    "这是一个测试",
			expected: "",
			notEmpty: true,
		},
		{
			name:     "英文文本",
			input:    "This is a test",
			expected: "",
			notEmpty: true,
		},
		{
			name:     "中英文混合",
			input:    "这是一个test文档",
			expected: "",
			notEmpty: true,
		},
		{
			name:     "包含标点符号",
			input:    "你好，世界！",
			expected: "",
			notEmpty: true,
		},
		{
			name:     "长文本",
			input:    "自然语言处理是人工智能领域的重要分支，它涉及计算机科学、语言学和数学等多个学科。",
			expected: "",
			notEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TokenizeWithSego(tt.input)

			if tt.input == "" {
				if result != "" {
					t.Errorf("Expected empty string for empty input, got: %s", result)
				}
				return
			}

			if tt.notEmpty {
				if result == "" {
					t.Errorf("Expected non-empty result, got empty string")
				}
				// 分词结果应该不包含原始文本中的所有字符（因为会被分词）
				// 但至少应该有一些内容
				if len(result) == 0 {
					t.Errorf("Tokenization result should not be empty")
				}
			}

			// 分词结果应该用空格分隔
			if result != "" && !strings.Contains(result, " ") && len(result) > len(tt.input) {
				// 如果结果比输入长很多，可能有问题
				if len(result) > len(tt.input)*2 {
					t.Errorf("Tokenization result seems too long: %s (input: %s)", result, tt.input)
				}
			}
		})
	}
}

func TestCreateFTSIndexWithSego(t *testing.T) {
	ctx := context.Background()

	t.Run("创建表并建立FTS索引", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_create.db")
		defer cleanupTestDB(t, db, dbPath)

		// 创建表
		_, err := db.ExecContext(ctx, `
			CREATE TABLE documents (
				id VARCHAR PRIMARY KEY,
				content TEXT
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// 创建 FTS 索引（应该自动创建 content_tokens 列）
		err = CreateFTSIndexWithSego(ctx, db, "documents", "id", "content", "")
		if err != nil {
			t.Fatalf("Failed to create FTS index: %v", err)
		}

		// 验证 content_tokens 列已创建
		var count int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) 
			FROM pragma_table_info('documents') 
			WHERE name = 'content_tokens'
		`).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check column: %v", err)
		}
		if count == 0 {
			t.Error("content_tokens column should be created")
		}
	})

	t.Run("使用自定义tokens列名", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_custom.db")
		defer cleanupTestDB(t, db, dbPath)

		_, err := db.ExecContext(ctx, `
			CREATE TABLE articles (
				id VARCHAR PRIMARY KEY,
				body TEXT
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// 使用自定义 tokens 列名
		err = CreateFTSIndexWithSego(ctx, db, "articles", "id", "body", "body_segmented")
		if err != nil {
			t.Fatalf("Failed to create FTS index: %v", err)
		}

		// 验证自定义列已创建
		var count int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) 
			FROM pragma_table_info('articles') 
			WHERE name = 'body_segmented'
		`).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check column: %v", err)
		}
		if count == 0 {
			t.Error("body_segmented column should be created")
		}
	})

	t.Run("tokens列已存在", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_existing.db")
		defer cleanupTestDB(t, db, dbPath)

		// 创建表时包含 tokens 列
		_, err := db.ExecContext(ctx, `
			CREATE TABLE docs (
				id VARCHAR PRIMARY KEY,
				content TEXT,
				content_tokens TEXT
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// 创建 FTS 索引（列已存在，不应报错）
		err = CreateFTSIndexWithSego(ctx, db, "docs", "id", "content", "content_tokens")
		if err != nil {
			t.Fatalf("Failed to create FTS index: %v", err)
		}
	})

	t.Run("重复创建索引", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_duplicate.db")
		defer cleanupTestDB(t, db, dbPath)

		_, err := db.ExecContext(ctx, `
			CREATE TABLE items (
				id VARCHAR PRIMARY KEY,
				text TEXT
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// 第一次创建
		err = CreateFTSIndexWithSego(ctx, db, "items", "id", "text", "")
		if err != nil {
			t.Fatalf("Failed to create FTS index first time: %v", err)
		}

		// 第二次创建（应该成功，因为会忽略已存在的错误）
		err = CreateFTSIndexWithSego(ctx, db, "items", "id", "text", "")
		if err != nil {
			t.Fatalf("Failed to create FTS index second time (should ignore duplicate): %v", err)
		}
	})
}

func TestUpdateContentTokens(t *testing.T) {
	ctx := context.Background()

	t.Run("正常更新分词结果", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_update.db")
		defer cleanupTestDB(t, db, dbPath)

		// 创建表
		_, err := db.ExecContext(ctx, `
			CREATE TABLE documents (
				id VARCHAR PRIMARY KEY,
				content TEXT,
				content_tokens TEXT
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// 插入文档
		docID := "doc1"
		content := "这是一个测试文档"
		_, err = db.ExecContext(ctx, `
			INSERT INTO documents (id, content) VALUES (?, ?)
		`, docID, content)
		if err != nil {
			t.Fatalf("Failed to insert document: %v", err)
		}

		// 更新分词结果
		err = UpdateContentTokens(ctx, db, "documents", "id", docID, "content", "content_tokens")
		if err != nil {
			t.Fatalf("Failed to update tokens: %v", err)
		}

		// 验证分词结果已更新
		var tokens string
		err = db.QueryRowContext(ctx, `
			SELECT content_tokens FROM documents WHERE id = ?
		`, docID).Scan(&tokens)
		if err != nil {
			t.Fatalf("Failed to query tokens: %v", err)
		}

		if tokens == "" {
			t.Error("Tokens should not be empty")
		}
		if tokens == content {
			t.Error("Tokens should be different from original content")
		}
	})

	t.Run("使用默认tokens列名", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_update_default.db")
		defer cleanupTestDB(t, db, dbPath)

		_, err := db.ExecContext(ctx, `
			CREATE TABLE articles (
				id VARCHAR PRIMARY KEY,
				body TEXT,
				body_tokens TEXT
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		docID := "art1"
		content := "自然语言处理技术"
		_, err = db.ExecContext(ctx, `
			INSERT INTO articles (id, body) VALUES (?, ?)
		`, docID, content)
		if err != nil {
			t.Fatalf("Failed to insert: %v", err)
		}

		// 使用默认 tokens 列名（body + "_tokens" = body_tokens）
		err = UpdateContentTokens(ctx, db, "articles", "id", docID, "body", "")
		if err != nil {
			t.Fatalf("Failed to update tokens: %v", err)
		}

		var tokens string
		err = db.QueryRowContext(ctx, `
			SELECT body_tokens FROM articles WHERE id = ?
		`, docID).Scan(&tokens)
		if err != nil {
			t.Fatalf("Failed to query: %v", err)
		}

		if tokens == "" {
			t.Error("Tokens should be updated")
		}
	})

	t.Run("文档不存在", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_update_notfound.db")
		defer cleanupTestDB(t, db, dbPath)

		_, err := db.ExecContext(ctx, `
			CREATE TABLE docs (
				id VARCHAR PRIMARY KEY,
				content TEXT,
				content_tokens TEXT
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// 尝试更新不存在的文档
		err = UpdateContentTokens(ctx, db, "docs", "id", "nonexistent", "content", "content_tokens")
		if err == nil {
			t.Error("Expected error for nonexistent document")
		}
	})
}

func TestSearchWithSego(t *testing.T) {
	ctx := context.Background()

	setupTestTable := func(t *testing.T, db *sql.DB, tableName string) {
		_, err := db.ExecContext(ctx, fmt.Sprintf(`
			CREATE TABLE %s (
				id VARCHAR PRIMARY KEY,
				content TEXT,
				content_tokens TEXT
			)
		`, tableName))
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// 创建 FTS 索引
		err = CreateFTSIndexWithSego(ctx, db, tableName, "id", "content", "content_tokens")
		if err != nil {
			t.Fatalf("Failed to create FTS index: %v", err)
		}
	}

	insertTestDocs := func(t *testing.T, db *sql.DB, tableName string) {
		docs := []struct {
			id      string
			content string
		}{
			{"doc1", "自然语言处理是人工智能的重要分支"},
			{"doc2", "机器学习算法可以用于文本分类"},
			{"doc3", "深度学习在计算机视觉领域有广泛应用"},
			{"doc4", "中文分词是自然语言处理的基础"},
		}

		for _, doc := range docs {
			tokens := TokenizeWithSego(doc.content)
			_, err := db.ExecContext(ctx, fmt.Sprintf(`
				INSERT INTO %s (id, content, content_tokens) VALUES (?, ?, ?)
			`, tableName), doc.id, doc.content, tokens)
			if err != nil {
				t.Fatalf("Failed to insert document %s: %v", doc.id, err)
			}
		}
	}

	t.Run("使用tokens列搜索", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_search.db")
		defer cleanupTestDB(t, db, dbPath)

		setupTestTable(t, db, "documents")
		insertTestDocs(t, db, "documents")

		// 搜索
		ids, err := SearchWithSego(ctx, db, "documents", "自然语言", "content", "content_tokens", 10)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(ids) == 0 {
			t.Error("Expected to find at least one document")
		}

		// 应该找到包含"自然语言"的文档
		found := false
		for _, id := range ids {
			if id == "doc1" || id == "doc4" {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Found IDs: %v", ids)
			t.Error("Expected to find doc1 or doc4 containing '自然语言'")
		}
	})

	t.Run("使用默认tokens列名", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_search_default.db")
		defer cleanupTestDB(t, db, dbPath)

		setupTestTable(t, db, "articles")
		insertTestDocs(t, db, "articles")

		// 使用默认 tokens 列名
		ids, err := SearchWithSego(ctx, db, "articles", "机器学习", "content", "", 10)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(ids) == 0 {
			t.Error("Expected to find documents")
		}
	})

	t.Run("空结果", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_search_empty.db")
		defer cleanupTestDB(t, db, dbPath)

		setupTestTable(t, db, "docs")
		insertTestDocs(t, db, "docs")

		// 搜索不存在的词
		ids, err := SearchWithSego(ctx, db, "docs", "不存在的关键词", "content", "content_tokens", 10)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		// 空结果是正常的
		if ids == nil {
			t.Error("Expected empty slice, not nil")
		}
	})

	t.Run("限制结果数量", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_search_limit.db")
		defer cleanupTestDB(t, db, dbPath)

		setupTestTable(t, db, "items")

		// 插入多个文档
		for i := 1; i <= 10; i++ {
			content := fmt.Sprintf("测试文档%d", i)
			tokens := TokenizeWithSego(content)
			_, err := db.ExecContext(ctx, `
				INSERT INTO items (id, content, content_tokens) VALUES (?, ?, ?)
			`, fmt.Sprintf("item%d", i), content, tokens)
			if err != nil {
				t.Fatalf("Failed to insert: %v", err)
			}
		}

		// 限制返回 3 个结果
		ids, err := SearchWithSego(ctx, db, "items", "测试", "content", "content_tokens", 3)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(ids) > 3 {
			t.Errorf("Expected at most 3 results, got %d", len(ids))
		}
	})

	t.Run("回退到content列搜索", func(t *testing.T) {
		db, dbPath := setupTestDB(t, "sego_fts_search_fallback.db")
		defer cleanupTestDB(t, db, dbPath)

		// 创建表但不包含 tokens 列
		_, err := db.ExecContext(ctx, `
			CREATE TABLE simple (
				id VARCHAR PRIMARY KEY,
				content TEXT
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// 插入数据
		_, err = db.ExecContext(ctx, `
			INSERT INTO simple (id, content) VALUES ('doc1', '测试内容')
		`)
		if err != nil {
			t.Fatalf("Failed to insert: %v", err)
		}

		// 搜索应该回退到使用 content 列
		ids, err := SearchWithSego(ctx, db, "simple", "测试", "content", "content_tokens", 10)
		// 注意：如果 FTS 索引不存在，可能会失败，这是正常的
		// 我们主要测试函数不会 panic
		if err != nil {
			t.Logf("Search failed (expected if FTS index not created): %v", err)
		} else if len(ids) > 0 {
			t.Logf("Found documents: %v", ids)
		}
	})
}

func TestSegoFTS_Integration(t *testing.T) {
	ctx := context.Background()
	db, dbPath := setupTestDB(t, "sego_fts_integration.db")
	defer cleanupTestDB(t, db, dbPath)

	// 创建表
	_, err := db.ExecContext(ctx, `
		CREATE TABLE documents (
			id VARCHAR PRIMARY KEY,
			content TEXT,
			content_tokens TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// 创建 FTS 索引
	err = CreateFTSIndexWithSego(ctx, db, "documents", "id", "content", "content_tokens")
	if err != nil {
		t.Fatalf("Failed to create FTS index: %v", err)
	}

	// 插入多个文档
	testDocs := []struct {
		id      string
		content string
	}{
		{"doc1", "人工智能是计算机科学的一个分支"},
		{"doc2", "机器学习使用算法从数据中学习模式"},
		{"doc3", "深度学习是机器学习的一个子领域"},
		{"doc4", "自然语言处理让计算机理解人类语言"},
		{"doc5", "计算机视觉用于图像识别和分析"},
	}

	for _, doc := range testDocs {
		tokens := TokenizeWithSego(doc.content)
		_, err = db.ExecContext(ctx, `
			INSERT INTO documents (id, content, content_tokens) VALUES (?, ?, ?)
		`, doc.id, doc.content, tokens)
		if err != nil {
			t.Fatalf("Failed to insert document %s: %v", doc.id, err)
		}
	}

	// 测试搜索
	t.Run("搜索人工智能", func(t *testing.T) {
		ids, err := SearchWithSego(ctx, db, "documents", "人工智能", "content", "content_tokens", 10)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(ids) == 0 {
			t.Error("Expected to find documents about AI")
		}
		t.Logf("Found %d documents: %v", len(ids), ids)
	})

	t.Run("搜索机器学习", func(t *testing.T) {
		ids, err := SearchWithSego(ctx, db, "documents", "机器学习", "content", "content_tokens", 10)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(ids) == 0 {
			t.Error("Expected to find documents about machine learning")
		}
		t.Logf("Found %d documents: %v", len(ids), ids)
	})

	// 测试更新分词结果
	t.Run("更新文档分词", func(t *testing.T) {
		newContent := "这是更新后的内容，包含新的关键词"
		_, err = db.ExecContext(ctx, `
			UPDATE documents SET content = ? WHERE id = 'doc1'
		`, newContent)
		if err != nil {
			t.Fatalf("Failed to update content: %v", err)
		}

		err = UpdateContentTokens(ctx, db, "documents", "id", "doc1", "content", "content_tokens")
		if err != nil {
			t.Fatalf("Failed to update tokens: %v", err)
		}

		var tokens string
		err = db.QueryRowContext(ctx, `
			SELECT content_tokens FROM documents WHERE id = 'doc1'
		`).Scan(&tokens)
		if err != nil {
			t.Fatalf("Failed to query tokens: %v", err)
		}

		if tokens == "" {
			t.Error("Tokens should be updated")
		}
		t.Logf("Updated tokens: %s", tokens)
	})
}
