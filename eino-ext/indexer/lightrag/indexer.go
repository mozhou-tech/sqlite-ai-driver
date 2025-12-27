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
	"fmt"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/rxdb-go/pkg/lightrag"
)

// IndexerConfig defines the configuration for the LightRAG indexer.
type IndexerConfig struct {
	// LightRAG is the LightRAG instance to use for indexing.
	LightRAG *lightrag.LightRAG
	// DocumentToMap optionally overrides the default conversion from eino document to map.
	DocumentToMap func(ctx context.Context, doc *schema.Document) (map[string]any, error)
}

// Indexer implements the Eino indexer.Indexer interface for LightRAG.
type Indexer struct {
	config *IndexerConfig
}

// NewIndexer creates a new LightRAG indexer.
func NewIndexer(ctx context.Context, config *IndexerConfig) (*Indexer, error) {
	if config.LightRAG == nil {
		return nil, fmt.Errorf("[NewIndexer] lightrag instance not provided")
	}

	if config.DocumentToMap == nil {
		config.DocumentToMap = defaultDocumentToMap
	}

	return &Indexer{
		config: config,
	}, nil
}

// Store indexes the provided documents into LightRAG.
func (i *Indexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) (ids []string, err error) {
	ctx = callbacks.EnsureRunInfo(ctx, i.GetType(), components.ComponentOfIndexer)
	ctx = callbacks.OnStart(ctx, &indexer.CallbackInput{Docs: docs})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	toStore := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		docMap, err := i.config.DocumentToMap(ctx, doc)
		if err != nil {
			return nil, fmt.Errorf("[Store] failed to convert document to map: %w", err)
		}
		toStore = append(toStore, docMap)
	}

	if ids, err = i.config.LightRAG.InsertBatch(ctx, toStore); err != nil {
		return nil, fmt.Errorf("[Store] failed to insert batch into lightrag: %w", err)
	}

	callbacks.OnEnd(ctx, &indexer.CallbackOutput{IDs: ids})

	return ids, nil
}

// GetType returns the component type.
func (i *Indexer) GetType() string {
	return "LightRAG"
}

// IsCallbacksEnabled returns true as this component supports callbacks.
func (i *Indexer) IsCallbacksEnabled() bool {
	return true
}

func defaultDocumentToMap(ctx context.Context, doc *schema.Document) (map[string]any, error) {
	docMap := make(map[string]any, len(doc.MetaData)+2)
	docMap["id"] = doc.ID
	docMap["content"] = doc.Content
	for k, v := range doc.MetaData {
		docMap[k] = v
	}
	return docMap, nil
}
