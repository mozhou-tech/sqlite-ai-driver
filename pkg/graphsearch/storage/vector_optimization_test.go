package storage_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch/storage"
)

// setupTestStore 设置测试用的 graphsearch 实例
func setupTestStore(t *testing.T, embedder interface{}) interface {
	Initialize(ctx context.Context) error
	Close() error
	AddEntity(ctx context.Context, entityID, entityName string, metadata map[string]any) error
	Link(ctx context.Context, subject, predicate, object string) error
	OptimizedSemanticSearch(ctx context.Context, query string, limit int, maxDepth int, optimizer interface{}) ([]interface{}, error)
	PreloadEmbeddings(ctx context.Context, optimizer interface{}, limit int) error
} {
	t.Helper()

	store, err := graphsearch.New(graphsearch.Options{
		Embedder:   embedder.(graphsearch.Embedder),
		WorkingDir: "./testdata",
		TableName:  "test_entities",
	})
	if err != nil {
		t.Fatalf("创建 graphsearch 实例失败: %v", err)
	}

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("初始化 graphsearch 失败: %v", err)
	}

	return store
}

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
	embedder := graphsearch.NewSimpleEmbedder(768)
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
	embedder := graphsearch.NewSimpleEmbedder(768)
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
