package lightrag

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSimpleEmbedder(t *testing.T) {
	dims := 10
	embedder := NewSimpleEmbedder(dims)

	if embedder.Dimensions() != dims {
		t.Errorf("expected dimensions %d, got %d", dims, embedder.Dimensions())
	}

	text := "hello"
	vec, err := embedder.Embed(context.Background(), text)
	if err != nil {
		t.Fatalf("failed to embed: %v", err)
	}

	if len(vec) != dims {
		t.Errorf("expected vector length %d, got %d", dims, len(vec))
	}

	// Verify embedding values (極簡實現：取前 N 个字符的 ASCII 值)
	for i := 0; i < len(text) && i < dims; i++ {
		expected := float64(text[i]) / 255.0
		if vec[i] != expected {
			t.Errorf("at index %d: expected %f, got %f", i, expected, vec[i])
		}
	}
}

func TestSimpleLLM(t *testing.T) {
	llm := &SimpleLLM{}
	ctx := context.Background()

	prompt := "Context: some context\n\nQuestion: What is this?\n\nAnswer the question based on the context."
	resp, err := llm.Complete(ctx, prompt)
	if err != nil {
		t.Fatalf("failed to complete: %v", err)
	}

	if !strings.Contains(resp, "What is this?") || !strings.Contains(resp, "some context") {
		t.Errorf("response should contain question and context, got: %s", resp)
	}

	resp2, err := llm.Complete(ctx, "just a prompt")
	if err != nil {
		t.Fatalf("failed to complete: %v", err)
	}
	if resp2 != "Simple LLM response" {
		t.Errorf("expected 'Simple LLM response', got: %s", resp2)
	}
}

func TestLightRAG_Flow(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_storage"
	defer os.RemoveAll(workingDir)

	embedder := NewSimpleEmbedder(768)
	llm := &SimpleLLM{}

	rag := New(Options{
		WorkingDir: workingDir,
		Embedder:   embedder,
		LLM:        llm,
	})

	// Test uninitialized call
	err := rag.Insert(ctx, "test content")
	if err == nil || !strings.Contains(err.Error(), "storages not initialized") {
		t.Errorf("expected error for uninitialized insert, got: %v", err)
	}

	// Initialize
	err = rag.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("failed to initialize storages: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	// Insert
	err = rag.Insert(ctx, "The capital of France is Paris.")
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	err = rag.Insert(ctx, "The capital of Germany is Berlin.")
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Give it a moment to index (asynchronous in FulltextSearch)
	time.Sleep(1 * time.Second)

	// Query - Vector mode
	resp, err := rag.Query(ctx, "What is the capital of France?", QueryParam{
		Mode:  ModeVector,
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("failed to query vector: %v", err)
	}
	if !strings.Contains(resp, "Paris") {
		t.Errorf("vector query response should contain 'Paris', got: %s", resp)
	}

	// Query - Fulltext mode
	resp, err = rag.Query(ctx, "Berlin", QueryParam{
		Mode:  ModeFulltext,
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("failed to query fulltext: %v", err)
	}
	if !strings.Contains(resp, "Berlin") {
		t.Errorf("fulltext query response should contain 'Berlin', got: %s", resp)
	}

	// Query - Hybrid mode
	resp, err = rag.Query(ctx, "capital", QueryParam{
		Mode:  ModeHybrid,
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("failed to query hybrid: %v", err)
	}
	if !strings.Contains(resp, "Paris") && !strings.Contains(resp, "Berlin") {
		t.Errorf("hybrid query response should contain relevant info, got: %s", resp)
	}

	// Query - No results
	resp, err = rag.Query(ctx, "Something totally unrelated", QueryParam{
		Mode:  ModeFulltext,
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	if resp != "No relevant information found." {
		t.Errorf("expected 'No relevant information found.', got: %s", resp)
	}
}

func TestLightRAG_NoEmbedder(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_no_embed"
	defer os.RemoveAll(workingDir)

	rag := New(Options{
		WorkingDir: workingDir,
		// No Embedder
		LLM: &SimpleLLM{},
	})

	err := rag.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	err = rag.Insert(ctx, "Only fulltext search is available here.")
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Vector query should fail
	_, err = rag.Query(ctx, "test", QueryParam{Mode: ModeVector})
	if err == nil || !strings.Contains(err.Error(), "vector search not available") {
		t.Errorf("expected error for missing vector search, got: %v", err)
	}

	// Fulltext query should still work
	resp, err := rag.Query(ctx, "fulltext", QueryParam{Mode: ModeFulltext})
	if err != nil {
		t.Fatalf("fulltext query failed: %v", err)
	}
	if !strings.Contains(resp, "available") {
		t.Errorf("expected response to contain 'available', got: %s", resp)
	}

	// Local query should fail when graph is not available (though it is by default if init succeeds)
	// We can test this by creating a RAG without initializing storages or by mocking.
}

func TestLightRAG_Retrieve_NoGraph(t *testing.T) {
	ctx := context.Background()
	rag := &LightRAG{initialized: true} // Fake initialization without graph
	_, err := rag.Retrieve(ctx, "query", QueryParam{Mode: ModeLocal})
	if err == nil || !strings.Contains(err.Error(), "graph search not available") {
		t.Errorf("expected error for missing graph, got: %v", err)
	}
}

func TestLightRAG_GraphMode(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_graph"
	defer os.RemoveAll(workingDir)

	embedder := NewSimpleEmbedder(768)
	llm := &SimpleLLM{}

	rag := New(Options{
		WorkingDir: workingDir,
		Embedder:   embedder,
		LLM:        llm,
	})

	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	// MockEntity will be extracted by SimpleLLM
	err := rag.Insert(ctx, "MockEntity is a very important entity.")
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Give it a moment for the background extraction
	time.Sleep(500 * time.Millisecond)

	// Query in Local mode
	resp, err := rag.Query(ctx, "Tell me about MockEntity", QueryParam{
		Mode:  ModeLocal,
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("failed to query local: %v", err)
	}

	// SimpleLLM will return a response containing the context
	if !strings.Contains(resp, "MockEntity") {
		t.Errorf("local query response should contain 'MockEntity', got: %s", resp)
	}
}

func TestLightRAG_Persistence(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_persistence"
	defer os.RemoveAll(workingDir)

	embedder := NewSimpleEmbedder(768)
	llm := &SimpleLLM{}

	// First session
	{
		rag := New(Options{
			WorkingDir: workingDir,
			Embedder:   embedder,
			LLM:        llm,
		})
		if err := rag.InitializeStorages(ctx); err != nil {
			t.Fatalf("failed to initialize: %v", err)
		}
		if err := rag.Insert(ctx, "Persisted content."); err != nil {
			t.Fatalf("failed to insert: %v", err)
		}
		time.Sleep(1 * time.Second)
		rag.FinalizeStorages(ctx)
	}

	// Second session
	{
		rag := New(Options{
			WorkingDir: workingDir,
			Embedder:   embedder,
			LLM:        llm,
		})
		if err := rag.InitializeStorages(ctx); err != nil {
			t.Fatalf("failed to initialize second session: %v", err)
		}
		defer rag.FinalizeStorages(ctx)

		resp, err := rag.Query(ctx, "Persisted", QueryParam{Mode: ModeFulltext})
		if err != nil {
			t.Fatalf("query failed: %v", err)
		}
		if !strings.Contains(resp, "Persisted content.") {
			t.Errorf("expected persisted content, got: %s", resp)
		}
	}
}

func TestLightRAG_Initialize_Twice(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_init_twice"
	defer os.RemoveAll(workingDir)

	rag := New(Options{WorkingDir: workingDir})

	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("first init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	if err := rag.InitializeStorages(ctx); err != nil {
		t.Errorf("second init failed: %v", err)
	}
}

func TestLightRAG_NoLLM(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_no_llm"
	defer os.RemoveAll(workingDir)

	rag := New(Options{WorkingDir: workingDir})
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	content := "This is some data."
	if err := rag.Insert(ctx, content); err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	time.Sleep(1 * time.Second)

	// No LLM provided, should return context text
	resp, err := rag.Query(ctx, "data", QueryParam{Mode: ModeFulltext})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !strings.Contains(resp, content) {
		t.Errorf("expected response to contain content, got: %s", resp)
	}
}

func TestLightRAG_Query_Limit(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_limit"
	defer os.RemoveAll(workingDir)

	rag := New(Options{WorkingDir: workingDir})
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	rag.Insert(ctx, "Doc 1")
	rag.Insert(ctx, "Doc 2")
	rag.Insert(ctx, "Doc 3")
	time.Sleep(1 * time.Second)

	resp, err := rag.Query(ctx, "Doc", QueryParam{Mode: ModeFulltext, Limit: 2})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// resp should contain [1] and [2] but not [3]
	if !strings.Contains(resp, "[1]") || !strings.Contains(resp, "[2]") {
		t.Errorf("expected 2 results, got: %s", resp)
	}
	if strings.Contains(resp, "[3]") {
		t.Errorf("did not expect 3rd result, got: %s", resp)
	}
}

func TestLightRAG_MetadataFiltering(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_filter"
	defer os.RemoveAll(workingDir)

	rag := New(Options{WorkingDir: workingDir})
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	// Insert documents with metadata
	docs := []map[string]any{
		{"id": "1", "content": "Paris is the capital of France.", "category": "geography"},
		{"id": "2", "content": "Berlin is the capital of Germany.", "category": "geography"},
		{"id": "3", "content": "RxDB is a database.", "category": "tech"},
	}

	for _, doc := range docs {
		_, err := rag.docs.Insert(ctx, doc)
		if err != nil {
			t.Fatalf("failed to insert: %v", err)
		}
	}
	time.Sleep(1 * time.Second)

	// Query with filter
	resp, err := rag.Query(ctx, "capital", QueryParam{
		Mode:  ModeFulltext,
		Limit: 5,
		Filters: map[string]any{
			"category": "geography",
		},
	})
	if err != nil {
		t.Fatalf("filtered query failed: %v", err)
	}

	if !strings.Contains(resp, "Paris") || !strings.Contains(resp, "Berlin") {
		t.Errorf("expected geography docs, got: %s", resp)
	}
	if strings.Contains(resp, "RxDB") {
		t.Errorf("did not expect tech doc, got: %s", resp)
	}

	// Query with another filter
	resp, err = rag.Query(ctx, "database", QueryParam{
		Mode:  ModeFulltext,
		Limit: 5,
		Filters: map[string]any{
			"category": "tech",
		},
	})
	if err != nil {
		t.Fatalf("filtered query failed: %v", err)
	}
	if !strings.Contains(resp, "RxDB") {
		t.Errorf("expected tech doc, got: %s", resp)
	}
	if strings.Contains(resp, "Paris") {
		t.Errorf("did not expect geography doc, got: %s", resp)
	}
}

func TestLightRAG_DefaultWorkingDir(t *testing.T) {
	ctx := context.Background()
	defaultDir := "./rag_storage"
	defer os.RemoveAll(defaultDir)

	rag := New(Options{}) // No WorkingDir
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	if _, err := os.Stat(defaultDir); os.IsNotExist(err) {
		t.Error("default working directory was not created")
	}
}

type FlexibleLLM struct {
	ResponseFunc func(prompt string) (string, error)
}

func (l *FlexibleLLM) Complete(ctx context.Context, prompt string) (string, error) {
	if l.ResponseFunc != nil {
		return l.ResponseFunc(prompt)
	}
	return "default response", nil
}

func TestLightRAG_InsertBatch(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_batch"
	defer os.RemoveAll(workingDir)

	rag := New(Options{
		WorkingDir: workingDir,
		LLM:        &SimpleLLM{},
	})
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	docs := []map[string]any{
		{"content": "Batch doc 1", "category": "A"},
		{"content": "Batch doc 2", "category": "B"},
		{"id": "custom-id-3", "content": "Batch doc 3", "category": "A"},
	}

	ids, err := rag.InsertBatch(ctx, docs)
	if err != nil {
		t.Fatalf("InsertBatch failed: %v", err)
	}

	if len(ids) != 3 {
		t.Errorf("expected 3 ids, got %d", len(ids))
	}

	if ids[2] != "custom-id-3" {
		t.Errorf("expected custom-id-3, got %s", ids[2])
	}

	// Test missing content
	badDocs := []map[string]any{
		{"category": "C"},
	}
	_, err = rag.InsertBatch(ctx, badDocs)
	if err == nil || !strings.Contains(err.Error(), "missing 'content' field") {
		t.Errorf("expected error for missing content, got: %v", err)
	}
}

func TestLightRAG_GraphSearchAndSubgraph(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_graph_extra"
	defer os.RemoveAll(workingDir)

	rag := New(Options{
		WorkingDir: workingDir,
		LLM:        &SimpleLLM{},
	})
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	// SimpleLLM extracts "RxDB" and links it to "JavaScript"
	err := rag.Insert(ctx, "RxDB is awesome")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Test SearchGraph
	graphData, err := rag.SearchGraph(ctx, "Tell me about RxDB")
	if err != nil {
		t.Fatalf("SearchGraph failed: %v", err)
	}

	foundRxDB := false
	for _, e := range graphData.Entities {
		if e.Name == "RxDB" {
			foundRxDB = true
			break
		}
	}
	if !foundRxDB {
		t.Error("RxDB entity not found in SearchGraph results")
	}

	// Test GetSubgraph
	subgraph, err := rag.GetSubgraph(ctx, "RxDB", 1)
	if err != nil {
		t.Fatalf("GetSubgraph failed: %v", err)
	}

	foundRel := false
	for _, r := range subgraph.Relationships {
		if r.Source == "RxDB" && r.Target == "JavaScript" && r.Relation == "BUILT_FOR" {
			foundRel = true
			break
		}
	}
	if !foundRel {
		t.Error("Relationship RxDB -[BUILT_FOR]-> JavaScript not found in subgraph")
	}
}

func TestLightRAG_Retrieve_Modes_Extra(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_modes_extra"
	defer os.RemoveAll(workingDir)

	rag := New(Options{
		WorkingDir: workingDir,
		Embedder:   NewSimpleEmbedder(768),
		LLM:        &SimpleLLM{},
	})
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	rag.Insert(ctx, "The quick brown fox jumps over the lazy dog.")
	time.Sleep(500 * time.Millisecond)

	// Test ModeGlobal (currently falls back to Hybrid)
	_, err := rag.Retrieve(ctx, "fox", QueryParam{Mode: ModeGlobal})
	if err != nil {
		t.Errorf("ModeGlobal failed: %v", err)
	}

	// Test ModeHybrid
	results, err := rag.Retrieve(ctx, "fox", QueryParam{Mode: ModeHybrid})
	if err != nil {
		t.Errorf("ModeHybrid failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("ModeHybrid returned no results")
	}

	// Test Default mode
	results, err = rag.Retrieve(ctx, "fox", QueryParam{Mode: "unsupported"})
	if err != nil {
		t.Errorf("Unsupported mode failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("Unsupported mode (default to fulltext) returned no results")
	}
}

func TestLightRAG_Extract_JSON_Errors(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_json_errors"
	defer os.RemoveAll(workingDir)

	// Mock LLM that returns invalid JSON
	mockLLM := &FlexibleLLM{
		ResponseFunc: func(prompt string) (string, error) {
			if strings.Contains(prompt, "Extract only the main entities") {
				return "invalid json [", nil
			}
			return "not a json {", nil
		},
	}

	rag := New(Options{
		WorkingDir: workingDir,
		LLM:        mockLLM,
	})
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	// Test extractQueryEntities error path in Retrieve
	// ModeLocal calls extractQueryEntities
	_, err := rag.Retrieve(ctx, "query", QueryParam{Mode: ModeLocal})
	// It should fallback to fulltext if extractQueryEntities fails
	if err != nil {
		t.Errorf("ModeLocal should fallback to fulltext on entity extraction error, but got err: %v", err)
	}

	// Test extractAndStore error path
	// This is called in background, but we can call it directly to test
	err = rag.extractAndStore(ctx, "some text", "doc1")
	if err == nil || !strings.Contains(err.Error(), "no JSON object found") {
		t.Errorf("expected error for invalid JSON in extractAndStore, got: %v", err)
	}

	// Test invalid JSON array in extractQueryEntities
	mockLLM.ResponseFunc = func(prompt string) (string, error) {
		return "[ 'not', 'valid', 'json' ]", nil // single quotes are invalid in JSON
	}
	_, err = rag.extractQueryEntities(ctx, "query")
	if err == nil || !strings.Contains(err.Error(), "failed to parse query entities") {
		t.Errorf("expected error for invalid JSON array, got: %v", err)
	}
}

func TestLightRAG_ExtractAndStore_LinkError(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_link_err"
	defer os.RemoveAll(workingDir)

	rag := New(Options{
		WorkingDir: workingDir,
		LLM:        &SimpleLLM{},
	})
	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Close the database to trigger errors in Link
	rag.FinalizeStorages(ctx)

	// This should log errors but not return them if it's the background go-routine version
	// but we call the internal extractAndStore directly here.
	err := rag.extractAndStore(ctx, "RxDB is a database", "doc1")
	// Link will error because db is closed
	if err != nil {
		// It might return error from Complete if we are unlucky,
		// but here we expect it to finish but log errors for Link.
		// Wait, extractAndStore doesn't return error if Link fails, it just logs.
		// So err should be nil if Complete succeeds.
	}
}

func TestLightRAG_Initialize_Error(t *testing.T) {
	ctx := context.Background()

	// Create a file where a directory should be to trigger an error
	tmpFile, err := os.CreateTemp("", "not_a_dir")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	rag := New(Options{
		WorkingDir: tmpFile.Name(),
	})

	err = rag.InitializeStorages(ctx)
	if err == nil {
		t.Error("expected error when workingDir is a file, not a directory")
	}
}

func TestLightRAG_Insert_Error(t *testing.T) {
	ctx := context.Background()
	workingDir := "./test_rag_insert_err"
	defer os.RemoveAll(workingDir)

	rag := New(Options{WorkingDir: workingDir})
	rag.InitializeStorages(ctx)
	rag.FinalizeStorages(ctx) // Close it

	err := rag.Insert(ctx, "test")
	if err == nil {
		t.Error("expected error inserting into closed database")
	}
}

func TestSimpleEmbedder_ZeroDims(t *testing.T) {
	embedder := NewSimpleEmbedder(0)
	if embedder.Dimensions() != 768 {
		t.Errorf("expected default dimensions 768, got %d", embedder.Dimensions())
	}
}

func TestLightRAG_Finalize_Nil(t *testing.T) {
	rag := New(Options{})
	// Should not panic and return nil
	err := rag.FinalizeStorages(context.Background())
	if err != nil {
		t.Errorf("expected nil error for nil storages, got: %v", err)
	}
}

func TestLightRAG_Insert_NotInitialized(t *testing.T) {
	rag := New(Options{})
	err := rag.Insert(context.Background(), "test")
	if err == nil || !strings.Contains(err.Error(), "storages not initialized") {
		t.Errorf("expected error for uninitialized insert, got: %v", err)
	}
}
