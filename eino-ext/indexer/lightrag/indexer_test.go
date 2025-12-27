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

	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/rxdb-go/pkg/lightrag"
)

func TestIndexer(t *testing.T) {
	ctx := context.Background()
	workingDir := "./rag_storage_test_indexer"
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
