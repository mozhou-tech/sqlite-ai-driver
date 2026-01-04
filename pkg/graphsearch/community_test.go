package graphsearch_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestSummarizeCommunity_EntityLevel(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	// 使用 Mock LLM
	mockLLM := &MockLLMGenerator{}
	options := graphsearch.DefaultCommunitySummarizationOptions(mockLLM)
	options.Granularity = graphsearch.GranularityEntity

	summary, err := store.SummarizeCommunity(ctx, "entity1", options)
	if err != nil {
		t.Fatalf("SummarizeCommunity() error = %v", err)
	}

	if summary == nil {
		t.Fatal("SummarizeCommunity() 返回 nil，期望非 nil")
	}

	if summary.CommunityID != "entity1" {
		t.Errorf("期望 CommunityID = entity1, 实际 = %s", summary.CommunityID)
	}

	if summary.Granularity != graphsearch.GranularityEntity {
		t.Errorf("期望 Granularity = GranularityEntity, 实际 = %s", summary.Granularity)
	}
}

func TestSummarizeCommunity_CommunityLevel(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity3", "Charlie", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")
	store.Link(ctx, "entity2", "knows", "entity3")

	// 使用 Mock LLM
	mockLLM := &MockLLMGenerator{}
	options := graphsearch.DefaultCommunitySummarizationOptions(mockLLM)
	options.Granularity = graphsearch.GranularityCommunity

	summary, err := store.SummarizeCommunity(ctx, "entity1", options)
	if err != nil {
		t.Fatalf("SummarizeCommunity() error = %v", err)
	}

	if summary == nil {
		t.Fatal("SummarizeCommunity() 返回 nil，期望非 nil")
	}

	if len(summary.Entities) == 0 {
		t.Error("期望社区中有实体")
	}
}

func TestSummarizeCommunities_Batch(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	// 使用 Mock LLM
	mockLLM := &MockLLMGenerator{}
	options := graphsearch.DefaultCommunitySummarizationOptions(mockLLM)

	summaries, err := store.SummarizeCommunities(ctx, []string{"entity1", "entity2"}, options)
	if err != nil {
		t.Fatalf("SummarizeCommunities() error = %v", err)
	}

	if len(summaries) == 0 {
		t.Error("期望至少有一个社区摘要")
	}
}

func TestDetectCommunities(t *testing.T) {
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

	communities, err := store.DetectCommunities(ctx, 2)
	if err != nil {
		t.Fatalf("DetectCommunities() error = %v", err)
	}

	if len(communities) < 2 {
		t.Errorf("期望至少 2 个社区，实际 %d", len(communities))
	}
}
