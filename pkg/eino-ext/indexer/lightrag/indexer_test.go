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
	"github.com/cloudwego/eino/schema"
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

func TestIndexer(t *testing.T) {
	ctx := context.Background()
	workingDir := "./rag_storage_test_indexer"
	os.RemoveAll(workingDir)
	defer os.RemoveAll(workingDir)

	// Create directories for databases
	if err := os.MkdirAll(workingDir, 0755); err != nil {
		t.Fatalf("Failed to create working directory: %v", err)
	}
	duckdbPath := filepath.Join(workingDir, "duckdb.db")
	graphPath := filepath.Join(workingDir, "graph.db")

	rag, err := New(Options{
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

	idx, err := NewIndexer(ctx, &IndexerConfig{
		LightRAG: rag,
	})
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	docs := []*schema.Document{
		{
			ID:      "1",
			Content: "Hello world",
			MetaData: map[string]any{
				"source": "test",
			},
		},
		{
			ID:      "2",
			Content: "Eino is great",
		},
	}

	ids, err := idx.Store(ctx, docs)
	if err != nil {
		t.Fatalf("failed to store documents: %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("expected 2 ids, got %d", len(ids))
	}

	if ids[0] != "1" || ids[1] != "2" {
		t.Errorf("expected ids [1, 2], got %v", ids)
	}
}
