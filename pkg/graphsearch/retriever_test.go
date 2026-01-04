package graphsearch_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestRetriever_HeuristicRetrieve(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	retriever := store.GetRetriever()
	processor := store.GetQueryProcessor()

	// 处理查询
	structuredQuery, err := processor.ProcessQuery(ctx, "查找 Alice")
	if err != nil {
		t.Fatalf("ProcessQuery() error = %v", err)
	}

	tests := []struct {
		name    string
		options *graphsearch.RetrievalOptions
		wantErr bool
	}{
		{
			name: "默认选项",
			options: &graphsearch.RetrievalOptions{
				Limit:    10,
				MaxDepth: 2,
			},
			wantErr: false,
		},
		{
			name: "限制深度",
			options: &graphsearch.RetrievalOptions{
				Limit:    5,
				MaxDepth: 1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := retriever.HeuristicRetrieve(ctx, structuredQuery, tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("HeuristicRetrieve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == nil {
				t.Error("HeuristicRetrieve() 返回 nil，期望非 nil")
			}
		})
	}
}

func TestRetriever_LearningRetrieve(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	retriever := store.GetRetriever()
	processor := store.GetQueryProcessor()

	// 处理查询
	structuredQuery, err := processor.ProcessQuery(ctx, "查找 Alice")
	if err != nil {
		t.Fatalf("ProcessQuery() error = %v", err)
	}

	tests := []struct {
		name    string
		options *graphsearch.RetrievalOptions
		wantErr bool
	}{
		{
			name: "默认选项",
			options: &graphsearch.RetrievalOptions{
				Limit:    10,
				MaxDepth: 2,
			},
			wantErr: false,
		},
		{
			name: "相似度阈值",
			options: &graphsearch.RetrievalOptions{
				Limit:               10,
				MaxDepth:            2,
				SimilarityThreshold: 0.5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := retriever.LearningRetrieve(ctx, structuredQuery, tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("LearningRetrieve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == nil {
				t.Error("LearningRetrieve() 返回 nil，期望非 nil")
			}
		})
	}
}

func TestRetriever_Retrieve(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	// 添加测试数据
	store.AddEntity(ctx, "entity1", "Alice", map[string]any{"type": "person"})
	store.AddEntity(ctx, "entity2", "Bob", map[string]any{"type": "person"})
	store.Link(ctx, "entity1", "knows", "entity2")

	retriever := store.GetRetriever()
	processor := store.GetQueryProcessor()

	// 处理查询
	structuredQuery, err := processor.ProcessQuery(ctx, "查找 Alice")
	if err != nil {
		t.Fatalf("ProcessQuery() error = %v", err)
	}

	tests := []struct {
		name     string
		strategy graphsearch.RetrievalStrategy
		wantErr  bool
	}{
		{
			name:     "启发式策略",
			strategy: graphsearch.StrategyHeuristicOnly,
			wantErr:  false,
		},
		{
			name:     "学习式策略",
			strategy: graphsearch.StrategyLearningOnly,
			wantErr:  false,
		},
		{
			name:     "混合策略",
			strategy: graphsearch.StrategyHybrid,
			wantErr:  false,
		},
		{
			name:     "自适应策略",
			strategy: graphsearch.StrategyAdaptive,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &graphsearch.RetrievalOptions{
				Limit:    10,
				MaxDepth: 2,
				Strategy: tt.strategy,
			}
			result, err := retriever.Retrieve(ctx, structuredQuery, options)
			if (err != nil) != tt.wantErr {
				t.Errorf("Retrieve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == nil {
				t.Error("Retrieve() 返回 nil，期望非 nil")
			}
		})
	}
}
