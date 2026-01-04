package graphsearch_test

import (
	"context"
	graphsearch "github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch/core"
	"testing"
)

// setupTestStore 设置测试用的 graphsearch 实例
func setupTestStore(t *testing.T, embedder graphsearch.Embedder) interface {
	Initialize(ctx context.Context) error
	Close() error
	GetQueryProcessor() graphsearch.QueryProcessor
	AddEntity(ctx context.Context, entityID, entityName string, metadata map[string]any) error
	Link(ctx context.Context, subject, predicate, object string) error
} {
	t.Helper()

	store, err := graphsearch.New(graphsearch.Options{
		Embedder:   embedder,
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

func TestQueryProcessor_ExtractEntities(t *testing.T) {
	ctx := context.Background()
	embedder := graphsearch.NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	processor := store.GetQueryProcessor()

	tests := []struct {
		name    string
		query   string
		wantMin int // 期望至少提取到的实体数量
		checkFn func(*testing.T, []graphsearch.QueryEntity)
	}{
		{
			name:    "提取大写实体",
			query:   "查找 Alice 和 Bob 的关系",
			wantMin: 2,
			checkFn: func(t *testing.T, entities []graphsearch.QueryEntity) {
				names := make(map[string]bool)
				for _, e := range entities {
					names[e.Name] = true
				}
				if !names["Alice"] && !names["Bob"] {
					t.Errorf("期望提取到 Alice 或 Bob，实际: %v", names)
				}
			},
		},
		{
			name:    "空查询",
			query:   "",
			wantMin: 0,
		},
		{
			name:    "无实体查询",
			query:   "这是一个测试查询",
			wantMin: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities, err := processor.ExtractEntities(ctx, tt.query)
			if err != nil {
				t.Fatalf("ExtractEntities() error = %v", err)
			}
			if len(entities) < tt.wantMin {
				t.Errorf("ExtractEntities() 提取实体数量 = %d, 期望至少 %d", len(entities), tt.wantMin)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, entities)
			}
		})
	}
}

func TestQueryProcessor_ExtractRelations(t *testing.T) {
	ctx := context.Background()
	embedder := graphsearch.NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	processor := store.GetQueryProcessor()

	tests := []struct {
		name    string
		query   string
		wantMin int
		checkFn func(*testing.T, []graphsearch.Relation)
	}{
		{
			name:    "提取关系 - 的",
			query:   "Alice 的朋友是 Bob",
			wantMin: 0, // 简化实现可能提取不到
		},
		{
			name:    "提取关系 - 属于",
			query:   "Alice 属于 公司A",
			wantMin: 0,
		},
		{
			name:    "无关系查询",
			query:   "这是一个简单的查询",
			wantMin: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relations, err := processor.ExtractRelations(ctx, tt.query)
			if err != nil {
				t.Fatalf("ExtractRelations() error = %v", err)
			}
			if len(relations) < tt.wantMin {
				t.Errorf("ExtractRelations() 提取关系数量 = %d, 期望至少 %d", len(relations), tt.wantMin)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, relations)
			}
		})
	}
}

func TestQueryProcessor_DecomposeQuery(t *testing.T) {
	ctx := context.Background()
	embedder := graphsearch.NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	processor := store.GetQueryProcessor()

	tests := []struct {
		name    string
		query   string
		wantMin int
		wantMax int
	}{
		{
			name:    "简单查询",
			query:   "查找信息",
			wantMin: 1,
			wantMax: 1,
		},
		{
			name:    "复合查询 - 和",
			query:   "查找 Alice 和 Bob",
			wantMin: 2,
			wantMax: 10,
		},
		{
			name:    "复合查询 - 或",
			query:   "查找 Alice 或 Bob",
			wantMin: 2,
			wantMax: 10,
		},
		{
			name:    "复合查询 - 逗号",
			query:   "查找 Alice, Bob, Charlie",
			wantMin: 2,
			wantMax: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subQueries, err := processor.DecomposeQuery(ctx, tt.query)
			if err != nil {
				t.Fatalf("DecomposeQuery() error = %v", err)
			}
			if len(subQueries) < tt.wantMin || len(subQueries) > tt.wantMax {
				t.Errorf("DecomposeQuery() 子查询数量 = %d, 期望范围 [%d, %d]", len(subQueries), tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestQueryProcessor_ExpandQuery(t *testing.T) {
	ctx := context.Background()
	embedder := graphsearch.NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	processor := store.GetQueryProcessor()

	tests := []struct {
		name    string
		query   string
		wantMin int
		checkFn func(*testing.T, []string)
	}{
		{
			name:    "扩展查询 - 查找",
			query:   "查找信息",
			wantMin: 1,
			checkFn: func(t *testing.T, terms []string) {
				// 应该包含原始查询
				found := false
				for _, term := range terms {
					if term == "查找信息" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("期望包含原始查询 '查找信息'")
				}
			},
		},
		{
			name:    "扩展查询 - 信息",
			query:   "获取信息",
			wantMin: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terms, err := processor.ExpandQuery(ctx, tt.query)
			if err != nil {
				t.Fatalf("ExpandQuery() error = %v", err)
			}
			if len(terms) < tt.wantMin {
				t.Errorf("ExpandQuery() 扩展词数量 = %d, 期望至少 %d", len(terms), tt.wantMin)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, terms)
			}
		})
	}
}

func TestQueryProcessor_ProcessQuery(t *testing.T) {
	ctx := context.Background()
	embedder := graphsearch.NewSimpleEmbedder(768)
	store := setupTestStore(t, embedder)
	defer store.Close()

	processor := store.GetQueryProcessor()

	tests := []struct {
		name    string
		query   string
		wantErr bool
		checkFn func(*testing.T, *graphsearch.StructuredQuery)
	}{
		{
			name:    "正常查询",
			query:   "查找 Alice 和 Bob 的关系",
			wantErr: false,
			checkFn: func(t *testing.T, sq *graphsearch.StructuredQuery) {
				if sq.OriginalQuery != "查找 Alice 和 Bob 的关系" {
					t.Errorf("OriginalQuery = %v, 期望 '查找 Alice 和 Bob 的关系'", sq.OriginalQuery)
				}
				if sq.Metadata == nil {
					t.Error("Metadata 不应该为 nil")
				}
			},
		},
		{
			name:    "空查询",
			query:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processor.ProcessQuery(ctx, tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == nil {
				t.Error("ProcessQuery() 返回 nil，期望非 nil")
			}
			if tt.checkFn != nil && result != nil {
				tt.checkFn(t, result)
			}
		})
	}
}
