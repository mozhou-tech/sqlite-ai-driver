package graphsearch_test

import (
	"context"
	"fmt"
	"testing"
)

func TestFindShortestPath(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建路径：entity1 -> entity2 -> entity3
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity3", "Charlie", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")
	store.Link(ctx, "entity2", "knows", "entity3")

	path, err := store.FindShortestPath(ctx, "entity1", "entity3", 5)
	if err != nil {
		t.Fatalf("FindShortestPath() error = %v", err)
	}

	if len(path) != 3 {
		t.Errorf("期望路径长度 = 3, 实际 = %d", len(path))
	}

	if path[0] != "entity1" {
		t.Errorf("期望路径起点 = entity1, 实际 = %s", path[0])
	}

	if path[len(path)-1] != "entity3" {
		t.Errorf("期望路径终点 = entity3, 实际 = %s", path[len(path)-1])
	}
}

func TestFindShortestPath_NoPath(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建两个不连通的实体
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})

	_, err := store.FindShortestPath(ctx, "entity1", "entity2", 5)
	if err == nil {
		t.Error("期望 FindShortestPath() 返回错误，但返回 nil")
	}
}

func TestFindAllPaths(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建多条路径：entity1 -> entity2 -> entity3 和 entity1 -> entity3
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity3", "Charlie", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")
	store.Link(ctx, "entity2", "knows", "entity3")
	store.Link(ctx, "entity1", "knows", "entity3")

	paths, err := store.FindAllPaths(ctx, "entity1", "entity3", 5, 10)
	if err != nil {
		t.Fatalf("FindAllPaths() error = %v", err)
	}

	if len(paths) == 0 {
		t.Error("期望至少找到一条路径")
	}
}

func TestGetCentralEntities(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建图：entity1 是中心节点
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity3", "Charlie", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity4", "David", map[string]any{"type": "person"})

	store.Link(ctx, "entity1", "knows", "entity2")
	store.Link(ctx, "entity1", "knows", "entity3")
	store.Link(ctx, "entity1", "knows", "entity4")

	central, err := store.GetCentralEntities(ctx, 5)
	if err != nil {
		t.Fatalf("GetCentralEntities() error = %v", err)
	}

	if len(central) == 0 {
		t.Error("期望至少有一个中心实体")
	}

	// entity1 应该有最高的度
	if central[0] != "entity1" {
		t.Logf("注意：entity1 可能不是第一个中心实体，实际 = %s", central[0])
	}
}

func TestGetEntityNeighbors(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建图
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity3", "Charlie", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")
	store.Link(ctx, "entity1", "knows", "entity3")

	neighbors, err := store.GetEntityNeighbors(ctx, "entity1", 10)
	if err != nil {
		t.Fatalf("GetEntityNeighbors() error = %v", err)
	}

	if len(neighbors) != 2 {
		t.Errorf("期望邻居数量 = 2, 实际 = %d", len(neighbors))
	}
}

func TestGetEntityNeighbors_MaxLimit(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 创建多个邻居
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	for i := 2; i <= 10; i++ {
		entityID := fmt.Sprintf("entity%d", i)
		store.AddEntity(ctx, entityID, fmt.Sprintf("Person%d", i), map[string]any{"type": "person"})
		store.Link(ctx, "entity1", "knows", entityID)
	}

	neighbors, err := store.GetEntityNeighbors(ctx, "entity1", 5)
	if err != nil {
		t.Fatalf("GetEntityNeighbors() error = %v", err)
	}

	if len(neighbors) > 5 {
		t.Errorf("期望邻居数量 <= 5, 实际 = %d", len(neighbors))
	}
}
