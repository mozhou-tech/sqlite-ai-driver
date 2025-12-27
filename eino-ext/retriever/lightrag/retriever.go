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
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/rxdb-go/pkg/lightrag"
)

// RetrieverConfig defines the configuration for the LightRAG retriever.
type RetrieverConfig struct {
	// LightRAG is the LightRAG instance to use for retrieval.
	LightRAG *lightrag.LightRAG
	// TopK limits the number of results returned, default 5.
	TopK int
	// Mode is the retrieval mode, default ModeHybrid.
	Mode lightrag.QueryMode
}

// Retriever implements the Eino retriever.Retriever interface for LightRAG.
type Retriever struct {
	config *RetrieverConfig
}

// NewRetriever creates a new LightRAG retriever.
func NewRetriever(ctx context.Context, config *RetrieverConfig) (*Retriever, error) {
	if config.LightRAG == nil {
		return nil, fmt.Errorf("[NewRetriever] lightrag instance not provided")
	}

	if config.TopK <= 0 {
		config.TopK = 5
	}

	if config.Mode == "" {
		config.Mode = lightrag.ModeHybrid
	}

	return &Retriever{
		config: config,
	}, nil
}

// Retrieve retrieves relevant documents from LightRAG.
func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) (docs []*schema.Document, err error) {
	co := retriever.GetCommonOptions(&retriever.Options{
		TopK: &r.config.TopK,
	}, opts...)

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

	param := lightrag.QueryParam{
		Mode:  r.config.Mode,
		Limit: *co.TopK,
	}
	if co.ScoreThreshold != nil {
		param.Threshold = *co.ScoreThreshold
	}

	results, err := r.config.LightRAG.Retrieve(ctx, query, param)
	if err != nil {
		return nil, fmt.Errorf("[Retrieve] lightrag retrieval failed: %w", err)
	}

	docs = make([]*schema.Document, 0, len(results))
	for _, res := range results {
		doc := &schema.Document{
			ID:       res.ID,
			Content:  res.Content,
			MetaData: res.Metadata,
		}
		docs = append(docs, doc)
	}

	callbacks.OnEnd(ctx, &retriever.CallbackOutput{Docs: docs})

	return docs, nil
}

// GetType returns the component type.
func (r *Retriever) GetType() string {
	return "LightRAG"
}

// IsCallbacksEnabled returns true as this component supports callbacks.
func (r *Retriever) IsCallbacksEnabled() bool {
	return true
}
