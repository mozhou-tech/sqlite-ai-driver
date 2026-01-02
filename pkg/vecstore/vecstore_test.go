package vecstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockEmbedder 用于测试的简单嵌入生成器
type mockEmbedder struct {
	dimensions int
}

func newMockEmbedder(dims int) *mockEmbedder {
	if dims <= 0 {
		dims = 128
	}
	return &mockEmbedder{dimensions: dims}
}

func (e *mockEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	vec := make([]float64, e.dimensions)
	// 简单实现：基于文本内容生成固定向量
	for i := 0; i < len(text) && i < e.dimensions; i++ {
		vec[i] = float64(text[i]) / 255.0
	}
	// 填充剩余维度
	for i := len(text); i < e.dimensions; i++ {
		vec[i] = 0.1
	}
	return vec, nil
}

func (e *mockEmbedder) Dimensions() int {
	return e.dimensions
}

// setupTestStore 创建测试用的 VecStore，使用临时目录
func setupTestStore(t *testing.T, embedder Embedder) (*VecStore, func()) {
	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "vecstore_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// 创建 VecStore 实例
	store := New(Options{
		Embedder: embedder,
	})

	// 修改数据库路径为临时目录中的文件
	// 由于 VecStore.Initialize 硬编码了 "vecstore.db"，我们需要通过修改内部状态
	// 或者直接设置 db 字段。但更好的方式是让每个测试使用独立的数据库文件
	// 这里我们通过设置工作目录来实现
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	// 切换到临时目录
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// 清理函数
	cleanup := func() {
		// 关闭数据库连接（Close 内部会处理 nil 检查）
		store.Close()
		// 恢复工作目录
		os.Chdir(oldWd)
		// 清理临时目录和数据库文件
		dbPath := filepath.Join(tmpDir, "vecstore.db")
		os.Remove(dbPath)
		os.Remove(dbPath + "-shm")
		os.Remove(dbPath + "-wal")
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestNew(t *testing.T) {
	embedder := newMockEmbedder(128)
	store := New(Options{
		Embedder: embedder,
	})

	if store == nil {
		t.Fatal("New returned nil")
	}
	if store.embedder != embedder {
		t.Error("embedder not set correctly")
	}
	if store.tableName != "vecstore_documents" {
		t.Errorf("expected table name 'vecstore_documents', got '%s'", store.tableName)
	}
	if store.initialized {
		t.Error("store should not be initialized after New")
	}
}

func TestInitialize(t *testing.T) {
	embedder := newMockEmbedder(128)
	store, cleanup := setupTestStore(t, embedder)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if !store.initialized {
		t.Error("store should be initialized after Initialize")
	}

	// 测试重复初始化
	err = store.Initialize(ctx)
	if err != nil {
		t.Errorf("repeated Initialize should not fail: %v", err)
	}
}

func TestInsert_WithoutInitialization(t *testing.T) {
	embedder := newMockEmbedder(128)
	store := New(Options{
		Embedder: embedder,
	})

	ctx := context.Background()
	err := store.Insert(ctx, "test text", nil)
	if err == nil {
		t.Error("Insert should fail when not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestInsert_EmptyText(t *testing.T) {
	embedder := newMockEmbedder(128)
	store, cleanup := setupTestStore(t, embedder)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	err = store.Insert(ctx, "", nil)
	if err == nil {
		t.Error("Insert should fail with empty text")
	}
	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("expected 'cannot be empty' error, got: %v", err)
	}
}

func TestInsert_WithMetadata(t *testing.T) {
	embedder := newMockEmbedder(128)
	store, cleanup := setupTestStore(t, embedder)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	metadata := map[string]any{
		"source": "test",
		"type":   "document",
		"id":     123,
	}

	err = store.Insert(ctx, "test document content", metadata)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// 等待embedding处理完成
	time.Sleep(500 * time.Millisecond)
}

func TestInsert_WithoutEmbedder(t *testing.T) {
	store, cleanup := setupTestStore(t, nil)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	err = store.Insert(ctx, "test text", nil)
	if err != nil {
		t.Fatalf("Insert should work without embedder: %v", err)
	}
}

func TestSearch_WithoutInitialization(t *testing.T) {
	embedder := newMockEmbedder(128)
	store := New(Options{
		Embedder: embedder,
	})

	ctx := context.Background()
	results, err := store.Search(ctx, "query", 10, nil)
	if err == nil {
		t.Error("Search should fail when not initialized")
	}
	if results != nil {
		t.Error("Search should return nil results when failed")
	}
}

func TestSearch_WithoutEmbedder(t *testing.T) {

	store := New(Options{
		Embedder: nil,
	})

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer store.Close()

	results, err := store.Search(ctx, "query", 10, nil)
	if err == nil {
		t.Error("Search should fail without embedder")
	}
	if !strings.Contains(err.Error(), "embedder not provided") {
		t.Errorf("expected 'embedder not provided' error, got: %v", err)
	}
	if results != nil {
		t.Error("Search should return nil results when failed")
	}
}

func TestSearch_Basic(t *testing.T) {
	embedder := newMockEmbedder(128)
	store, cleanup := setupTestStore(t, embedder)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// 插入文档
	err = store.Insert(ctx, "The capital of France is Paris", nil)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	err = store.Insert(ctx, "The capital of Germany is Berlin", nil)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// 等待embedding处理完成（SQLite 可能需要稍长时间）
	time.Sleep(2 * time.Second)

	// 搜索
	results, err := store.Search(ctx, "France capital", 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Search should return at least one result")
	}

	// 验证结果格式
	for _, result := range results {
		if result.ID == "" {
			t.Error("result ID should not be empty")
		}
		if result.Content == "" {
			t.Error("result Content should not be empty")
		}
		if result.Score < 0 || result.Score > 1 {
			t.Errorf("similarity score should be between 0 and 1, got: %f", result.Score)
		}
	}
}

func TestSearch_WithMetadataFilter(t *testing.T) {
	embedder := newMockEmbedder(128)
	store, cleanup := setupTestStore(t, embedder)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// 插入带metadata的文档
	metadata1 := map[string]any{
		"category": "geography",
		"country":  "France",
	}
	err = store.Insert(ctx, "The capital of France is Paris", metadata1)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	metadata2 := map[string]any{
		"category": "geography",
		"country":  "Germany",
	}
	err = store.Insert(ctx, "The capital of Germany is Berlin", metadata2)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// 等待embedding处理完成（SQLite 可能需要稍长时间）
	time.Sleep(2 * time.Second)

	// 使用metadata过滤搜索
	filter := MetadataFilter{
		"country": "France",
	}
	results, err := store.Search(ctx, "capital", 10, filter)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// 验证结果都符合过滤条件
	for _, result := range results {
		if result.Data != nil {
			if country, ok := result.Data["country"].(string); ok {
				if country != "France" {
					t.Errorf("result should have country='France', got: %s", country)
				}
			}
		}
	}
}

func TestSearch_Limit(t *testing.T) {
	embedder := newMockEmbedder(128)
	store, cleanup := setupTestStore(t, embedder)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// 插入多个文档
	for i := 0; i < 5; i++ {
		err = store.Insert(ctx, fmt.Sprintf("Document %d content", i), nil)
		if err != nil {
			t.Fatalf("Insert failed: %v", err)
		}
	}

	// 等待embedding处理完成（SQLite 可能需要稍长时间）
	time.Sleep(2 * time.Second)

	// 测试limit
	results, err := store.Search(ctx, "document", 3, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}

	// 测试默认limit（当limit <= 0时）
	results, err = store.Search(ctx, "document", 0, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) > 10 {
		t.Errorf("expected at most 10 results (default), got %d", len(results))
	}
}

func TestClose(t *testing.T) {
	embedder := newMockEmbedder(128)
	store, cleanup := setupTestStore(t, embedder)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	err = store.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 测试重复关闭
	err = store.Close()
	if err != nil {
		t.Errorf("repeated Close should not fail: %v", err)
	}
}

func TestProcessPendingEmbeddings(t *testing.T) {
	embedder := newMockEmbedder(128)
	store, cleanup := setupTestStore(t, embedder)
	defer cleanup()

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// 插入文档（会触发processPendingEmbeddings）
	err = store.Insert(ctx, "Test document for embedding", nil)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// 等待embedding处理（SQLite 可能需要稍长时间）
	time.Sleep(2 * time.Second)

	// 验证embedding已生成（通过搜索）
	results, err := store.Search(ctx, "test", 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Search should return results after embedding is processed")
	}
}
