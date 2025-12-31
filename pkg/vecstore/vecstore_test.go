package vecstore

import (
	"context"
	"fmt"
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
	store := New(Options{
		Embedder: embedder,
	})

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

	// 清理
	store.Close()
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
	store := New(Options{
		Embedder: embedder,
	})

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer store.Close()

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
	store := New(Options{
		Embedder: embedder,
	})

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer store.Close()

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

	store := New(Options{
		Embedder: nil,
	})

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer store.Close()

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
	store := New(Options{
		Embedder: embedder,
	})

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer store.Close()

	// 插入文档
	err = store.Insert(ctx, "The capital of France is Paris", nil)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	err = store.Insert(ctx, "The capital of Germany is Berlin", nil)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// 等待embedding处理完成
	time.Sleep(1 * time.Second)

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
	store := New(Options{
		Embedder: embedder,
	})

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer store.Close()

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

	// 等待embedding处理完成
	time.Sleep(1 * time.Second)

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
	store := New(Options{
		Embedder: embedder,
	})

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer store.Close()

	// 插入多个文档
	for i := 0; i < 5; i++ {
		err = store.Insert(ctx, fmt.Sprintf("Document %d content", i), nil)
		if err != nil {
			t.Fatalf("Insert failed: %v", err)
		}
	}

	// 等待embedding处理完成
	time.Sleep(1 * time.Second)

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
	store := New(Options{
		Embedder: embedder,
	})

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
	store := New(Options{
		Embedder: embedder,
	})

	ctx := context.Background()
	err := store.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer store.Close()

	// 插入文档（会触发processPendingEmbeddings）
	err = store.Insert(ctx, "Test document for embedding", nil)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// 等待embedding处理
	time.Sleep(1 * time.Second)

	// 验证embedding已生成（通过搜索）
	results, err := store.Search(ctx, "test", 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Search should return results after embedding is processed")
	}
}
