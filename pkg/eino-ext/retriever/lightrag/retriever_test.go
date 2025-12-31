/*
 * Copyright 2025 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package lightrag

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/indexer/lightrag"
)

type simpleEmbedder struct {
	dims int
}

func (e *simpleEmbedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	vectors := make([][]float64, len(texts))
	for i := range vectors {
		vectors[i] = make([]float64, e.dims)
		// Simple embedding: fill with constant values based on text length
		for j := range vectors[i] {
			vectors[i][j] = float64(len(texts[i])) * 0.01
		}
	}
	return vectors, nil
}

func TestRetriever(t *testing.T) {
	ctx := context.Background()
	workingDir := "./rag_storage_test_retriever"
	os.RemoveAll(workingDir)
	defer os.RemoveAll(workingDir)

	// Create directories for databases
	if err := os.MkdirAll(workingDir, 0755); err != nil {
		t.Fatalf("Failed to create working directory: %v", err)
	}
	duckdbPath := filepath.Join(workingDir, "duckdb.db")
	graphPath := filepath.Join(workingDir, "graph.db")

	rag, err := lightrag.New(lightrag.Options{
		WorkingDir: workingDir,
		DuckDBPath: duckdbPath,
		GraphPath:  graphPath,
		Embedder:   &simpleEmbedder{dims: 768},
		TableName:  "documents",
	})
	if err != nil {
		t.Fatalf("failed to create LightRAG: %v", err)
	}
	defer rag.Close()

	// Prepare data
	docs := []map[string]any{
		{
			"id":      "1",
			"content": "Hello world",
			"source":  "test",
		},
		{
			"id":      "2",
			"content": "Eino is a framework for building LLM applications",
		},
	}
	_, err = rag.InsertBatch(ctx, docs)
	if err != nil {
		t.Fatalf("failed to insert batch: %v", err)
	}

	ret, err := NewRetriever(ctx, &RetrieverConfig{
		LightRAG: rag,
		TopK:     1,
		Mode:     lightrag.QueryMode(lightrag.ModeFulltext),
	})
	if err != nil {
		t.Fatalf("failed to create retriever: %v", err)
	}

	// Test Retrieve
	results, err := ret.Retrieve(ctx, "Eino")
	if err != nil {
		t.Fatalf("failed to retrieve: %v", err)
	}

	if len(results) == 0 {
		t.Errorf("expected at least 1 result, got %d", len(results))
		return
	}

	// Check if we got the expected document
	found := false
	for _, result := range results {
		if result.ID == "2" {
			found = true
			if result.Content != "Eino is a framework for building LLM applications" {
				t.Errorf("unexpected content: %s", result.Content)
			}
			break
		}
	}

	if !found {
		t.Errorf("expected to find document with id 2, got results: %v", results)
	}
}
