package graphsearch_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestOrganizer_PruneGraph(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	organizer := store.GetOrganizer()

	// 创建测试子图
	subgraphs := []graphsearch.Subgraph{
		{
			RootEntity: "entity1",
			Triples: []graphsearch.Triple{
				{Subject: "entity1", Predicate: "knows", Object: "entity2"},
				{Subject: "entity1", Predicate: "knows", Object: "entity3"},
			},
			Entities: []string{"entity1", "entity2", "entity3"},
			Score:    0.8,
		},
		{
			RootEntity: "entity2",
			Triples: []graphsearch.Triple{
				{Subject: "entity2", Predicate: "works_at", Object: "company1"},
			},
			Entities: []string{"entity2", "company1"},
			Score:    0.5,
		},
	}

	tests := []struct {
		name    string
		options *graphsearch.PruningOptions
		wantErr bool
	}{
		{
			name: "限制节点数",
			options: &graphsearch.PruningOptions{
				MaxNodes:         2,
				MaxEdges:         10,
				MinScore:         0.0,
				KeepCoreEntities: true,
			},
			wantErr: false,
		},
		{
			name: "限制边数",
			options: &graphsearch.PruningOptions{
				MaxNodes:         10,
				MaxEdges:         1,
				MinScore:         0.0,
				KeepCoreEntities: true,
			},
			wantErr: false,
		},
		{
			name: "最小分数阈值",
			options: &graphsearch.PruningOptions{
				MaxNodes:         10,
				MaxEdges:         10,
				MinScore:         0.6,
				KeepCoreEntities: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := organizer.PruneGraph(ctx, subgraphs, tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("PruneGraph() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == nil {
				t.Error("PruneGraph() 返回 nil，期望非 nil")
			}
		})
	}
}

func TestOrganizer_Rerank(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	organizer := store.GetOrganizer()

	// 创建测试检索结果
	result := &graphsearch.RetrievalResult{
		Entities: []graphsearch.RetrievedEntity{
			{EntityID: "entity1", EntityName: "Alice", Score: 0.5},
			{EntityID: "entity2", EntityName: "Bob", Score: 0.8},
			{EntityID: "entity3", EntityName: "Charlie", Score: 0.3},
		},
		Triples: []graphsearch.Triple{
			{Subject: "entity1", Predicate: "knows", Object: "entity2"},
		},
		Subgraphs: []graphsearch.Subgraph{},
		Metadata:  make(map[string]any),
	}

	tests := []struct {
		name    string
		method  graphsearch.RerankingMethod
		wantErr bool
	}{
		{
			name:    "按分数排序",
			method:  graphsearch.RerankByScore,
			wantErr: false,
		},
		{
			name:    "按中心性排序",
			method:  graphsearch.RerankByCentrality,
			wantErr: false,
		},
		{
			name:    "按多样性排序",
			method:  graphsearch.RerankByDiversity,
			wantErr: false,
		},
		{
			name:    "混合排序",
			method:  graphsearch.RerankByHybrid,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &graphsearch.RerankingOptions{
				Method:            tt.method,
				TopK:              10,
				UseGraphStructure: true,
			}
			reranked, err := organizer.Rerank(ctx, result, options)
			if (err != nil) != tt.wantErr {
				t.Errorf("Rerank() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && reranked == nil {
				t.Error("Rerank() 返回 nil，期望非 nil")
			}
		})
	}
}

func TestOrganizer_Verbalize(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	organizer := store.GetOrganizer()

	// 创建测试子图
	subgraphs := []graphsearch.Subgraph{
		{
			RootEntity: "entity1",
			Triples: []graphsearch.Triple{
				{Subject: "entity1", Predicate: "knows", Object: "entity2"},
			},
			Entities: []string{"entity1", "entity2"},
			Score:    0.8,
		},
	}

	tests := []struct {
		name    string
		format  graphsearch.VerbalizationFormat
		wantErr bool
	}{
		{
			name:    "自然语言格式",
			format:  graphsearch.FormatNaturalLanguage,
			wantErr: false,
		},
		{
			name:    "结构化格式",
			format:  graphsearch.FormatStructured,
			wantErr: false,
		},
		{
			name:    "摘要格式",
			format:  graphsearch.FormatSummary,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &graphsearch.VerbalizationOptions{
				Format:          tt.format,
				IncludeMetadata: false,
				MaxLength:       500,
			}
			texts, err := organizer.Verbalize(ctx, subgraphs, options)
			if (err != nil) != tt.wantErr {
				t.Errorf("Verbalize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(texts) == 0 {
				t.Error("Verbalize() 返回空文本，期望非空")
			}
		})
	}
}

func TestOrganizer_Organize(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	organizer := store.GetOrganizer()

	// 创建测试检索结果
	result := &graphsearch.RetrievalResult{
		Entities: []graphsearch.RetrievedEntity{
			{EntityID: "entity1", EntityName: "Alice", Score: 0.8},
		},
		Triples: []graphsearch.Triple{
			{Subject: "entity1", Predicate: "knows", Object: "entity2"},
		},
		Subgraphs: []graphsearch.Subgraph{},
		Metadata:  make(map[string]any),
	}

	tests := []struct {
		name    string
		options *graphsearch.OrganizationOptions
		wantErr bool
	}{
		{
			name: "默认选项",
			options: &graphsearch.OrganizationOptions{
				EnablePruning:       true,
				EnableReranking:     true,
				EnableAugmentation:  false,
				EnableVerbalization: true,
			},
			wantErr: false,
		},
		{
			name: "禁用所有功能",
			options: &graphsearch.OrganizationOptions{
				EnablePruning:       false,
				EnableReranking:     false,
				EnableAugmentation:  false,
				EnableVerbalization: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			organized, err := organizer.Organize(ctx, result, tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("Organize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && organized == nil {
				t.Error("Organize() 返回 nil，期望非 nil")
			}
		})
	}
}
