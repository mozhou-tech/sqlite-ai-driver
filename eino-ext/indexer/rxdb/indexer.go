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

package rxdb

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/rxdb-go/pkg/rxdb"
)

type IndexerConfig struct {
	// Collection is an RxDB collection.
	Collection rxdb.Collection
	// DocumentToMap supports customize how to convert eino document to rxdb map.
	// It should return the map and a mapping of which fields in the map should be embedded.
	// The key in fieldsToEmbed is the field whose value (should be string) will be embedded,
	// and the value is the field name where the resulting vector should be stored.
	DocumentToMap func(ctx context.Context, doc *schema.Document) (map[string]any, map[string]string, error)
	// BatchSize controls embedding texts size.
	// Default 10.
	BatchSize int `json:"batch_size"`
	// Embedding vectorization method for values need to be embedded.
	Embedding embedding.Embedder
}

type Indexer struct {
	config *IndexerConfig
}

func NewIndexer(ctx context.Context, config *IndexerConfig) (*Indexer, error) {
	if config.Embedding == nil {
		return nil, fmt.Errorf("[NewIndexer] embedding not provided for rxdb indexer")
	}

	if config.Collection == nil {
		return nil, fmt.Errorf("[NewIndexer] rxdb collection not provided")
	}

	if config.DocumentToMap == nil {
		config.DocumentToMap = defaultDocumentToMap
	}

	if config.BatchSize == 0 {
		config.BatchSize = 10
	}

	return &Indexer{
		config: config,
	}, nil
}

func (i *Indexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) (ids []string, err error) {
	options := indexer.GetCommonOptions(&indexer.Options{
		Embedding: i.config.Embedding,
	}, opts...)

	ctx = callbacks.EnsureRunInfo(ctx, i.GetType(), components.ComponentOfIndexer)
	ctx = callbacks.OnStart(ctx, &indexer.CallbackInput{Docs: docs})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	if err = i.bulkStore(ctx, docs, options); err != nil {
		return nil, err
	}

	ids = make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}

	callbacks.OnEnd(ctx, &indexer.CallbackOutput{IDs: ids})

	return ids, nil
}

func (i *Indexer) bulkStore(ctx context.Context, docs []*schema.Document, options *indexer.Options) (err error) {
	emb := options.Embedding

	var (
		toStore []map[string]any
		texts   []string
		// metadata to track which fields in which document need embedding
		embedMeta []embedInfo
	)

	embAndAdd := func() error {
		if len(texts) > 0 {
			if emb == nil {
				return fmt.Errorf("[bulkStore] embedding method not provided")
			}

			vectors, err := emb.EmbedStrings(i.makeEmbeddingCtx(ctx, emb), texts)
			if err != nil {
				return fmt.Errorf("[bulkStore] embedding failed, %w", err)
			}

			if len(vectors) != len(texts) {
				return fmt.Errorf("[bulkStore] invalid vector length, expected=%d, got=%d", len(texts), len(vectors))
			}

			for _, info := range embedMeta {
				info.doc[info.vectorField] = vectors[info.textIdx]
			}
		}

		if _, err := i.config.Collection.BulkUpsert(ctx, toStore); err != nil {
			return fmt.Errorf("[bulkStore] rxdb bulk upsert failed: %w", err)
		}

		toStore = toStore[:0]
		texts = texts[:0]
		embedMeta = embedMeta[:0]

		return nil
	}

	for _, doc := range docs {
		docMap, fieldsToEmbed, err := i.config.DocumentToMap(ctx, doc)
		if err != nil {
			return err
		}

		embSize := len(fieldsToEmbed)
		if embSize > i.config.BatchSize {
			return fmt.Errorf("[bulkStore] embedding size over batch size, batch size=%d, got size=%d",
				i.config.BatchSize, embSize)
		}

		if len(texts)+embSize > i.config.BatchSize {
			if err = embAndAdd(); err != nil {
				return err
			}
		}

		for textField, vectorField := range fieldsToEmbed {
			val, ok := docMap[textField]
			if !ok {
				return fmt.Errorf("[bulkStore] text field %s not found in document map", textField)
			}

			text, ok := val.(string)
			if !ok {
				return fmt.Errorf("[bulkStore] text field %s is not a string", textField)
			}

			embedMeta = append(embedMeta, embedInfo{
				doc:         docMap,
				vectorField: vectorField,
				textIdx:     len(texts),
			})
			texts = append(texts, text)
		}

		toStore = append(toStore, docMap)
	}

	if len(toStore) > 0 {
		if err = embAndAdd(); err != nil {
			return err
		}
	}

	return nil
}

type embedInfo struct {
	doc         map[string]any
	vectorField string
	textIdx     int
}

func (i *Indexer) makeEmbeddingCtx(ctx context.Context, emb embedding.Embedder) context.Context {
	runInfo := &callbacks.RunInfo{
		Component: components.ComponentOfEmbedding,
	}

	if embType, ok := components.GetType(emb); ok {
		runInfo.Type = embType
	}

	runInfo.Name = runInfo.Type + string(runInfo.Component)

	return callbacks.ReuseHandlers(ctx, runInfo)
}

const typ = "RxDB"

func (i *Indexer) GetType() string {
	return typ
}

func (i *Indexer) IsCallbacksEnabled() bool {
	return true
}

func defaultDocumentToMap(ctx context.Context, doc *schema.Document) (map[string]any, map[string]string, error) {
	if doc.ID == "" {
		return nil, nil, fmt.Errorf("[defaultDocumentToMap] doc id not set")
	}

	docMap := make(map[string]any, len(doc.MetaData)+2)
	docMap["id"] = doc.ID
	docMap[defaultReturnFieldContent] = doc.Content
	for k, v := range doc.MetaData {
		docMap[k] = v
	}

	fieldsToEmbed := map[string]string{
		defaultReturnFieldContent: defaultReturnFieldVectorContent,
	}

	return docMap, fieldsToEmbed, nil
}
