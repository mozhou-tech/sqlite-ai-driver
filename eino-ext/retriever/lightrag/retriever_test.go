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
	"testing"
	"time"

	"github.com/mozhou-tech/rxdb-go/pkg/lightrag"
)

func TestRetriever(t *testing.T) {
	ctx := context.Background()
	workingDir := "./rag_storage_test_retriever"
	os.RemoveAll(workingDir)
	defer os.RemoveAll(workingDir)

	rag := lightrag.New(lightrag.Options{
		WorkingDir: workingDir,
		Embedder:   lightrag.NewSimpleEmbedder(768),
		LLM:        &lightrag.SimpleLLM{},
	})

	err := rag.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("failed to initialize storages: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

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

	// Wait for fulltext indexing (it's asynchronous via watchChanges)
	time.Sleep(500 * time.Millisecond)

	ret, err := NewRetriever(ctx, &RetrieverConfig{
		LightRAG: rag,
		TopK:     1,
		Mode:     lightrag.ModeFulltext,
	})
	if err != nil {
		t.Fatalf("failed to create retriever: %v", err)
	}

	// Test Retrieve
	results, err := ret.Retrieve(ctx, "Eino")
	if err != nil {
		t.Fatalf("failed to retrieve: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if results[0].ID != "2" {
		t.Errorf("expected id 2, got %s", results[0].ID)
	}

	if results[0].Content != "Eino is a framework for building LLM applications" {
		t.Errorf("unexpected content: %s", results[0].Content)
	}
}

