package storage_test

import (
	"context"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch/storage"
	"testing"
)

func TestVectorSearchOptimizer_New(t *testing.T) {
	optimizer := storage.NewVectorSearchOptimizer(100)
	if optimizer == nil {
		t.Fatal("NewVectorSearchOptimizer() 返回 nil")
	}

	if optimizer.GetCacheSize() != 0 {
		t.Error("期望初始缓存大小为 0")
	}
}

func TestVectorSearchOptimizer_ClearCache(t *testing.T) {
	optimizer := storage.NewVectorSearchOptimizer(100)

	// 清空缓存
	optimizer.ClearCache()

	if optimizer.GetCacheSize() != 0 {
		t.Error("期望清空后缓存大小为 0")
	}
}

func TestOptimizedSemanticSearch(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	optimizer := storage.NewVectorSearchOptimizer(100)

	results, err := store.OptimizedSemanticSearch(ctx, "Alice", 10, 2, optimizer)
	if err != nil {
		t.Fatalf("OptimizedSemanticSearch() error = %v", err)
	}

	if len(results) == 0 {
		t.Error("期望至少有一个搜索结果")
	}

	// 验证缓存是否被使用
	if optimizer.GetCacheSize() > 0 {
		t.Logf("缓存已使用，大小 = %d", optimizer.GetCacheSize())
	}
}

func TestPreloadEmbeddings(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})

	optimizer := storage.NewVectorSearchOptimizer(100)

	err := store.PreloadEmbeddings(ctx, optimizer, 10)
	if err != nil {
		t.Fatalf("PreloadEmbeddings() error = %v", err)
	}

	// 验证缓存是否已加载
	cacheSize := optimizer.GetCacheSize()
	if cacheSize == 0 {
		t.Error("期望预加载后缓存大小 > 0")
	}
}
