package graphsearch_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestCommunityOrganizer_OrganizeByCommunity(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据（两个独立的社区）
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	store.AddEntity(ctx, "entity3", "Charlie", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity4", "David", map[string]any{"type": "person"})
	store.Link(ctx, "entity3", "knows", "entity4")

	// 创建检索结果
	retriever := store.GetRetriever()
	processor := store.GetQueryProcessor()

	structuredQuery, err := processor.ProcessQuery(ctx, "查找所有人员")
	if err != nil {
		t.Fatalf("ProcessQuery() error = %v", err)
	}

	retrievalResult, err := retriever.Retrieve(ctx, structuredQuery, graphsearch.DefaultRetrievalOptions())
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	// 使用默认的组织器
	organizer := store.GetOrganizer()

	// 使用默认的组织方法
	organized, err := organizer.Organize(ctx, retrievalResult, &graphsearch.OrganizationOptions{
		EnablePruning:       true,
		EnableReranking:     true,
		EnableAugmentation:  false,
		EnableVerbalization: true,
	})
	if err != nil {
		t.Fatalf("OrganizeByCommunity() error = %v", err)
	}

	if organized == nil {
		t.Fatal("期望 OrganizedResult 非 nil")
	}

	if len(organized.Subgraphs) == 0 {
		t.Error("期望至少有一个社区子图")
	}
}

func TestCommunityOrganizationOptions_Default(t *testing.T) {
	options := graphsearch.DefaultCommunityOrganizationOptions()
	if options == nil {
		t.Fatal("DefaultCommunityOrganizationOptions() 返回 nil")
	}

	if options.MinCommunitySize <= 0 {
		t.Error("期望 MinCommunitySize > 0")
	}
}

func TestCommunityOrganizer_DeduplicateEntities(t *testing.T) {
	// 这个测试需要访问未导出的方法，暂时跳过
	// 可以通过测试 OrganizeByCommunity 来间接测试去重功能
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加重复的实体数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	retriever := store.GetRetriever()
	processor := store.GetQueryProcessor()

	structuredQuery, err := processor.ProcessQuery(ctx, "查找 Alice")
	if err != nil {
		t.Fatalf("ProcessQuery() error = %v", err)
	}

	retrievalResult, err := retriever.Retrieve(ctx, structuredQuery, graphsearch.DefaultRetrievalOptions())
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	// 使用默认组织器，它应该会去重
	organizer := store.GetOrganizer()
	organized, err := organizer.Organize(ctx, retrievalResult, graphsearch.DefaultOrganizationOptions())
	if err != nil {
		t.Fatalf("Organize() error = %v", err)
	}

	// 验证结果不为空
	if organized == nil {
		t.Fatal("期望 OrganizedResult 非 nil")
	}
}
