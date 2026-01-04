package graphsearch_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

// GraphSearch 测试接口，用于访问 graphsearch 的方法
type GraphSearch interface {
	Initialize(ctx context.Context) error
	Close() error
	GraphRAGQuery(ctx context.Context, query string, options *graphsearch.GraphRAGOptions) (*graphsearch.GeneratedAnswer, error)
	GraphRAGQueryWithOptions(ctx context.Context, query string, funcs ...func(*graphsearch.GraphRAGOptions)) (*graphsearch.GeneratedAnswer, error)
	GetQueryProcessor() graphsearch.QueryProcessor
	GetRetriever() graphsearch.Retriever
	GetOrganizer() graphsearch.Organizer
	GetGenerator() graphsearch.Generator
	AddEntity(ctx context.Context, entityID, entityName string, metadata map[string]any) error
	Link(ctx context.Context, subject, predicate, object string) error
	SemanticSearch(ctx context.Context, query string, limit int, maxDepth int) ([]graphsearch.SemanticSearchResult, error)
	GetSubgraph(ctx context.Context, entityID string, maxDepth int) ([]graphsearch.Triple, error)
	GetNeighbors(ctx context.Context, node, predicate string) ([]string, error)
	FindPath(ctx context.Context, from, to string, maxDepth int, predicate string) ([][]string, error)
	ListEntities(ctx context.Context, limit int, offset int) ([]map[string]any, error)
	// 新增方法的接口（用于测试新功能）
	IndexDocument(ctx context.Context, filePath string, options *graphsearch.IndexingOptions) error
	IndexDocuments(ctx context.Context, filePaths []string, options *graphsearch.IndexingOptions) error
	SummarizeCommunity(ctx context.Context, entityID string, options *graphsearch.CommunitySummarizationOptions) (*graphsearch.CommunitySummary, error)
	SummarizeCommunities(ctx context.Context, entityIDs []string, options *graphsearch.CommunitySummarizationOptions) ([]*graphsearch.CommunitySummary, error)
	DetectCommunities(ctx context.Context, maxDepth int) ([]graphsearch.Community, error)
	OptimizedSemanticSearch(ctx context.Context, query string, limit int, maxDepth int, optimizer *graphsearch.VectorSearchOptimizer) ([]graphsearch.SemanticSearchResult, error)
	PreloadEmbeddings(ctx context.Context, optimizer *graphsearch.VectorSearchOptimizer, limit int) error
	FindShortestPath(ctx context.Context, from, to string, maxDepth int) ([]string, error)
	FindAllPaths(ctx context.Context, from, to string, maxDepth int, maxPaths int) ([][]string, error)
	GetCentralEntities(ctx context.Context, limit int) ([]string, error)
	GetEntityNeighbors(ctx context.Context, entityID string, maxNeighbors int) ([]string, error)
}

// setupTestStore 创建并初始化测试用的 graphsearch 实例
func setupTestStore(t *testing.T, embedder graphsearch.Embedder) GraphSearch {
	t.Helper()

	store, err := graphsearch.New(graphsearch.Options{
		Embedder:   embedder,
		WorkingDir: "./testdata",
		TableName:  "test_entities",
	})
	if err != nil {
		t.Fatalf("Failed to create graphsearch: %v", err)
	}

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize graphsearch: %v", err)
	}

	return store
}

// cleanupTestStore 清理测试用的 graphsearch 实例
func cleanupTestStore(t *testing.T, store GraphSearch) {
	t.Helper()
	if store != nil {
		if err := store.Close(); err != nil {
			t.Logf("Failed to close graphsearch: %v", err)
		}
	}
}
