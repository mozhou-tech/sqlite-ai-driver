package cayley_driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGraphBasic(t *testing.T) {
	// 使用 testdata 目录作为 workingDir
	workingDir := "testdata"
	dbPath := "graph_basic.db"

	// 确保 testdata 目录存在
	if err := os.MkdirAll(workingDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		fullPath := filepath.Join(workingDir, "graph", dbPath)
		_ = os.Remove(fullPath)
	}()

	// 创建图数据库
	graph, err := NewGraphWithPrefix(workingDir, dbPath, "")
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	ctx := context.Background()

	// 测试 Link
	if err := graph.Link(ctx, "user1", "follows", "user2"); err != nil {
		t.Fatalf("Failed to link: %v", err)
	}

	if err := graph.Link(ctx, "user2", "follows", "user3"); err != nil {
		t.Fatalf("Failed to link: %v", err)
	}

	// 测试 GetNeighbors
	neighbors, err := graph.GetNeighbors(ctx, "user1", "follows")
	if err != nil {
		t.Fatalf("Failed to get neighbors: %v", err)
	}
	if len(neighbors) != 1 || neighbors[0] != "user2" {
		t.Errorf("Expected [user2], got %v", neighbors)
	}

	// 测试 GetInNeighbors
	inNeighbors, err := graph.GetInNeighbors(ctx, "user2", "follows")
	if err != nil {
		t.Fatalf("Failed to get in neighbors: %v", err)
	}
	if len(inNeighbors) != 1 || inNeighbors[0] != "user1" {
		t.Errorf("Expected [user1], got %v", inNeighbors)
	}

	// 测试 Query API
	query := graph.Query()
	results, err := query.V("user1").Out("follows").All(ctx)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
	if results[0].Subject != "user1" || results[0].Object != "user2" {
		t.Errorf("Expected {user1 follows user2}, got %v", results[0])
	}

	// 测试 Values
	values, err := query.V("user1").Out("follows").Values(ctx)
	if err != nil {
		t.Fatalf("Failed to get values: %v", err)
	}
	if len(values) != 1 || values[0] != "user2" {
		t.Errorf("Expected [user2], got %v", values)
	}

	// 测试 FindPath
	paths, err := graph.FindPath(ctx, "user1", "user3", 5, "follows")
	if err != nil {
		t.Fatalf("Failed to find path: %v", err)
	}
	if len(paths) == 0 {
		t.Error("Expected at least one path, got none")
	}
	if len(paths[0]) != 3 || paths[0][0] != "user1" || paths[0][2] != "user3" {
		t.Errorf("Expected path [user1 user2 user3], got %v", paths[0])
	}

	// 测试 Unlink
	if err := graph.Unlink(ctx, "user1", "follows", "user2"); err != nil {
		t.Fatalf("Failed to unlink: %v", err)
	}

	neighbors, err = graph.GetNeighbors(ctx, "user1", "follows")
	if err != nil {
		t.Fatalf("Failed to get neighbors: %v", err)
	}
	if len(neighbors) != 0 {
		t.Errorf("Expected empty neighbors after unlink, got %v", neighbors)
	}
}

func TestGraphMultiplePredicates(t *testing.T) {
	workingDir := "testdata"
	dbPath := "graph_predicates.db"

	if err := os.MkdirAll(workingDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		fullPath := filepath.Join(workingDir, "graph", dbPath)
		_ = os.Remove(fullPath)
	}()

	graph, err := NewGraphWithPrefix(workingDir, dbPath, "")
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	ctx := context.Background()

	// 创建多种关系
	graph.Link(ctx, "alice", "follows", "bob")
	graph.Link(ctx, "alice", "likes", "bob")
	graph.Link(ctx, "alice", "follows", "charlie")

	// 测试按 predicate 过滤
	follows, err := graph.GetNeighbors(ctx, "alice", "follows")
	if err != nil {
		t.Fatalf("Failed to get neighbors: %v", err)
	}
	if len(follows) != 2 {
		t.Errorf("Expected 2 follows, got %d", len(follows))
	}

	likes, err := graph.GetNeighbors(ctx, "alice", "likes")
	if err != nil {
		t.Fatalf("Failed to get neighbors: %v", err)
	}
	if len(likes) != 1 || likes[0] != "bob" {
		t.Errorf("Expected [bob], got %v", likes)
	}

	// 测试获取所有邻居
	all, err := graph.GetNeighbors(ctx, "alice", "")
	if err != nil {
		t.Fatalf("Failed to get all neighbors: %v", err)
	}
	if len(all) != 2 { // bob 和 charlie，去重后
		t.Errorf("Expected 2 unique neighbors, got %d", len(all))
	}
}

func TestGraphQueryChain(t *testing.T) {
	workingDir := "testdata"
	dbPath := "graph_query.db"

	if err := os.MkdirAll(workingDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}
	defer func() {
		fullPath := filepath.Join(workingDir, "graph", dbPath)
		_ = os.Remove(fullPath)
	}()

	graph, err := NewGraphWithPrefix(workingDir, dbPath, "")
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	ctx := context.Background()

	// 创建链式关系: A -> B -> C
	graph.Link(ctx, "A", "next", "B")
	graph.Link(ctx, "B", "next", "C")
	graph.Link(ctx, "C", "next", "D")

	// 测试多步查询
	query := graph.Query()
	values, err := query.V("A").Out("next").Out("next").Values(ctx)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	if len(values) != 1 || values[0] != "C" {
		t.Errorf("Expected [C], got %v", values)
	}

	// 测试 All 返回三元组
	results, err := query.V("A").Out("next").Out("next").All(ctx)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
	if results[0].Subject != "B" || results[0].Object != "C" {
		t.Errorf("Expected {B next C}, got %v", results[0])
	}
}
