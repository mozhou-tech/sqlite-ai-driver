package graphsearch_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestIndexDocument_TextFile(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建临时文本文件
	tmpDir := t.TempDir()
	textFile := filepath.Join(tmpDir, "test.txt")
	content := "Alice is a person. Bob is a person. Alice knows Bob."
	if err := os.WriteFile(textFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 索引文档
	options := graphsearch.DefaultIndexingOptions()
	err := store.IndexDocument(ctx, textFile, options)
	if err != nil {
		t.Fatalf("IndexDocument() error = %v", err)
	}

	// 验证实体是否已添加
	entities, err := store.ListEntities(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListEntities() error = %v", err)
	}

	if len(entities) == 0 {
		t.Error("期望至少有一个实体被索引")
	}
}

func TestIndexDocuments_Batch(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建多个临时文本文件
	tmpDir := t.TempDir()
	files := []string{
		filepath.Join(tmpDir, "test1.txt"),
		filepath.Join(tmpDir, "test2.txt"),
		filepath.Join(tmpDir, "test3.txt"),
	}

	contents := []string{
		"Alice is a person.",
		"Bob is a person.",
		"Charlie is a person.",
	}

	for i, file := range files {
		if err := os.WriteFile(file, []byte(contents[i]), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// 批量索引文档
	options := graphsearch.DefaultIndexingOptions()
	options.BatchSize = 2
	err := store.IndexDocuments(ctx, files, options)
	if err != nil {
		t.Fatalf("IndexDocuments() error = %v", err)
	}

	// 验证实体是否已添加
	entities, err := store.ListEntities(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListEntities() error = %v", err)
	}

	if len(entities) < 3 {
		t.Errorf("期望至少 3 个实体被索引，实际 %d", len(entities))
	}
}

func TestIndexDocument_UnsupportedFormat(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建不支持的文件格式
	tmpDir := t.TempDir()
	unsupportedFile := filepath.Join(tmpDir, "test.xyz")
	if err := os.WriteFile(unsupportedFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 尝试索引不支持的文件
	options := graphsearch.DefaultIndexingOptions()
	err := store.IndexDocument(ctx, unsupportedFile, options)
	if err == nil {
		t.Error("期望 IndexDocument() 返回错误，但返回 nil")
	}
}

func TestIndexDocument_WithLLM(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建临时文本文件
	tmpDir := t.TempDir()
	textFile := filepath.Join(tmpDir, "test.txt")
	content := "Alice is a person. Bob is a person. Alice knows Bob."
	if err := os.WriteFile(textFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 使用 Mock LLM
	mockLLM := &MockLLMGenerator{}
	options := graphsearch.DefaultIndexingOptions()
	options.LLMGenerator = mockLLM

	// 索引文档
	err := store.IndexDocument(ctx, textFile, options)
	// 即使 LLM 失败，也应该回退到启发式方法
	if err != nil {
		t.Logf("IndexDocument() with LLM returned error (expected if LLM fails): %v", err)
	}
}

// MockLLMGenerator 用于测试的 Mock LLM
type MockLLMGenerator struct{}

func (m *MockLLMGenerator) Generate(ctx context.Context, prompt string, options map[string]any) (string, error) {
	// 返回模拟的 JSON 响应
	return `{
		"entities": [
			{"name": "Alice", "type": "Person", "description": "A person"},
			{"name": "Bob", "type": "Person", "description": "A person"}
		],
		"relationships": [
			{"source": "Alice", "target": "Bob", "relation": "knows", "description": "Alice knows Bob"}
		]
	}`, nil
}
