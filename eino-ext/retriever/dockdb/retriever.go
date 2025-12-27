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
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/rxdb-go/pkg/rxdb"
)

type RetrieverConfig struct {
	// VectorSearch is an RxDB vector search instance.
	VectorSearch *rxdb.VectorSearch
	// ReturnFields limits the attributes returned from the document.
	// Default []string{"content", "vector_content"}
	ReturnFields []string
	// DocumentConverter converts retrieved raw document to eino Document, default defaultResultParser.
	DocumentConverter func(ctx context.Context, doc rxdb.Document) (*schema.Document, error)
	// TopK limits number of results given, default 5.
	TopK int
	// Embedding vectorization method for query.
	Embedding embedding.Embedder
}

type Retriever struct {
	config *RetrieverConfig
}

func NewRetriever(ctx context.Context, config *RetrieverConfig) (*Retriever, error) {
	if config.Embedding == nil {
		return nil, fmt.Errorf("[NewRetriever] embedding not provided for rxdb retriever")
	}

	if config.VectorSearch == nil {
		return nil, fmt.Errorf("[NewRetriever] rxdb vector search not provided")
	}

	if config.TopK == 0 {
		config.TopK = 5
	}

	if len(config.ReturnFields) == 0 {
		config.ReturnFields = []string{
			defaultReturnFieldContent,
			defaultReturnFieldVectorContent,
		}
	}

	if config.DocumentConverter == nil {
		config.DocumentConverter = defaultResultParser(config.ReturnFields)
	}

	return &Retriever{
		config: config,
	}, nil
}

func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) (docs []*schema.Document, err error) {
	co := retriever.GetCommonOptions(&retriever.Options{
		TopK:      &r.config.TopK,
		Embedding: r.config.Embedding,
	}, opts...)
	// io := retriever.GetImplSpecificOptions(&implOptions{}, opts...) // RxDB doesn't support filter query in VectorSearch yet

	ctx = callbacks.EnsureRunInfo(ctx, r.GetType(), components.ComponentOfRetriever)
	ctx = callbacks.OnStart(ctx, &retriever.CallbackInput{
		Query:          query,
		TopK:           *co.TopK,
		ScoreThreshold: co.ScoreThreshold,
	})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	emb := co.Embedding
	if emb == nil {
		return nil, fmt.Errorf("[rxdb retriever] embedding not provided")
	}

	vectors, err := emb.EmbedStrings(r.makeEmbeddingCtx(ctx, emb), []string{query})
	if err != nil {
		return nil, err
	}

	if len(vectors) != 1 {
		return nil, fmt.Errorf("[rxdb retriever] invalid return length of vector, got=%d, expected=1", len(vectors))
	}

	searchOptions := rxdb.VectorSearchOptions{
		Limit: *co.TopK,
	}
	if co.ScoreThreshold != nil {
		searchOptions.MinScore = *co.ScoreThreshold
	}

	results, err := r.config.VectorSearch.Search(ctx, vectors[0], searchOptions)
	if err != nil {
		return nil, fmt.Errorf("[rxdb retriever] search failed: %w", err)
	}

	for _, result := range results {
		doc, err := r.config.DocumentConverter(ctx, result.Document)
		if err != nil {
			return nil, err
		}
		if doc.MetaData == nil {
			doc.MetaData = make(map[string]any)
		}
		doc.MetaData[SortByDistanceAttributeName] = result.Distance
		docs = append(docs, doc)
	}

	callbacks.OnEnd(ctx, &retriever.CallbackOutput{Docs: docs})

	return docs, nil
}

func (r *Retriever) makeEmbeddingCtx(ctx context.Context, emb embedding.Embedder) context.Context {
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

func (r *Retriever) GetType() string {
	return typ
}

func (r *Retriever) IsCallbacksEnabled() bool {
	return true
}

func defaultResultParser(returnFields []string) func(ctx context.Context, doc rxdb.Document) (*schema.Document, error) {
	return func(ctx context.Context, doc rxdb.Document) (*schema.Document, error) {
		data := doc.Data()
		resp := &schema.Document{
			ID:       doc.ID(),
			Content:  "",
			MetaData: map[string]any{},
		}

		for _, field := range returnFields {
			val, found := data[field]
			if !found {
				continue
			}

			if field == defaultReturnFieldContent {
				if s, ok := val.(string); ok {
					resp.Content = s
				}
			} else if field == defaultReturnFieldVectorContent {
				if v, ok := val.([]float64); ok {
					resp.WithDenseVector(v)
				}
			} else {
				resp.MetaData[field] = val
			}
		}

		return resp, nil
	}
}
