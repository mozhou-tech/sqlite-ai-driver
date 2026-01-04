package graphsearch_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestGenerator_GenerateWithGraph(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	generator := store.GetGenerator()

	// 创建测试组织结果
	organizedResult := &graphsearch.OrganizedResult{
		Entities: []graphsearch.RetrievedEntity{
			{EntityID: "entity1", EntityName: "Alice", Score: 0.8},
		},
		Triples: []graphsearch.Triple{
			{Subject: "entity1", Predicate: "knows", Object: "entity2"},
		},
		Subgraphs:       []graphsearch.Subgraph{},
		VerbalizedTexts: []string{"实体 Alice 的相关信息：\nentity1 knows entity2"},
		Metadata:        make(map[string]any),
	}

	tests := []struct {
		name    string
		method  graphsearch.GenerationMethod
		wantErr bool
	}{
		{
			name:    "基于图的方法",
			method:  graphsearch.MethodGraph,
			wantErr: false,
		},
		{
			name:    "基于判别的方法",
			method:  graphsearch.MethodDiscrimination,
			wantErr: false,
		},
		{
			name:    "混合方法",
			method:  graphsearch.MethodHybrid,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &graphsearch.GenerationOptions{
				Method:          tt.method,
				MaxLength:       500,
				Temperature:     0.7,
				IncludeSources:  true,
				UseGraphContext: true,
			}
			answer, err := generator.Generate(ctx, organizedResult, "查询文本", options)
			if (err != nil) != tt.wantErr {
				t.Errorf("Generate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && answer == nil {
				t.Error("Generate() 返回 nil，期望非 nil")
			}
			if !tt.wantErr && answer != nil && answer.Answer == "" {
				t.Error("Generate() 返回空答案，期望非空")
			}
		})
	}
}

func TestGenerator_GenerateWithGraph_Method(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	generator := store.GetGenerator()

	organizedResult := &graphsearch.OrganizedResult{
		Entities: []graphsearch.RetrievedEntity{
			{EntityID: "entity1", EntityName: "Alice", Score: 0.8},
		},
		Triples: []graphsearch.Triple{
			{Subject: "entity1", Predicate: "knows", Object: "entity2"},
		},
		Subgraphs: []graphsearch.Subgraph{},
		Metadata:  make(map[string]any),
	}

	options := &graphsearch.GenerationOptions{
		Method:          graphsearch.MethodGraph,
		MaxLength:       500,
		Temperature:     0.7,
		IncludeSources:  true,
		UseGraphContext: true,
	}

	answer, err := generator.GenerateWithGraph(ctx, organizedResult, "查询文本", options)
	if err != nil {
		t.Fatalf("GenerateWithGraph() error = %v", err)
	}
	if answer == nil {
		t.Fatal("GenerateWithGraph() 返回 nil，期望非 nil")
	}
	if answer.Answer == "" {
		t.Error("GenerateWithGraph() 返回空答案，期望非空")
	}
}
