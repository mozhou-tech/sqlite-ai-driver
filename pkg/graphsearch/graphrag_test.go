package graphsearch_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestGraphRAGQuery_Basic(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	// 基本查询
	answer, err := store.GraphRAGQuery(ctx, "查找 Alice", nil)
	if err != nil {
		t.Fatalf("GraphRAGQuery() error = %v", err)
	}
	if answer == nil {
		t.Fatal("GraphRAGQuery() 返回 nil，期望非 nil")
	}
	if answer.Answer == "" {
		t.Error("GraphRAGQuery() 返回空答案，期望非空")
	}
}

func TestGraphRAGQuery_WithOptions(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	tests := []struct {
		name    string
		options *graphsearch.GraphRAGOptions
		wantErr bool
	}{
		{
			name:    "默认选项",
			options: nil,
			wantErr: false,
		},
		{
			name: "自定义选项",
			options: &graphsearch.GraphRAGOptions{
				RetrievalOptions: &graphsearch.RetrievalOptions{
					Limit:    5,
					MaxDepth: 1,
					Strategy: graphsearch.StrategyHybrid,
				},
			},
			wantErr: false,
		},
		{
			name: "仅启发式检索",
			options: &graphsearch.GraphRAGOptions{
				RetrievalOptions: &graphsearch.RetrievalOptions{
					Limit:    5,
					MaxDepth: 1,
					Strategy: graphsearch.StrategyHeuristicOnly,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answer, err := store.GraphRAGQuery(ctx, "查找 Alice", tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("GraphRAGQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && answer == nil {
				t.Error("GraphRAGQuery() 返回 nil，期望非 nil")
			}
		})
	}
}

func TestGraphRAGQuery_WithConvenienceFunctions(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	// 使用便捷函数
	answer, err := store.GraphRAGQueryWithOptions(ctx, "查找 Alice",
		graphsearch.WithRetrievalStrategy(graphsearch.StrategyHybrid),
		graphsearch.WithRetrievalLimit(5),
		graphsearch.WithRetrievalMaxDepth(2),
	)
	if err != nil {
		t.Fatalf("GraphRAGQueryWithOptions() error = %v", err)
	}
	if answer == nil {
		t.Fatal("GraphRAGQueryWithOptions() 返回 nil，期望非 nil")
	}
}

func TestGraphRAGQuery_Validation(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "空查询",
			query:   "",
			wantErr: true,
		},
		{
			name:    "正常查询",
			query:   "查找信息",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.GraphRAGQuery(ctx, tt.query, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("GraphRAGQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGraphRAGQuery_Components(t *testing.T) {
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 测试获取组件
	processor := store.GetQueryProcessor()
	if processor == nil {
		t.Error("GetQueryProcessor() 返回 nil")
	}

	retriever := store.GetRetriever()
	if retriever == nil {
		t.Error("GetRetriever() 返回 nil")
	}

	organizer := store.GetOrganizer()
	if organizer == nil {
		t.Error("GetOrganizer() 返回 nil")
	}

	generator := store.GetGenerator()
	if generator == nil {
		t.Error("GetGenerator() 返回 nil")
	}
}
