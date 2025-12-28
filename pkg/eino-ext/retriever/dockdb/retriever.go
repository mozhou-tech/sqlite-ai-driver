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

package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

type RetrieverConfig struct {
	// DB is an already opened DuckDB database connection instance.
	// This retriever does not open or close the database connection.
	// The caller is responsible for managing the database connection lifecycle.
	DB *sql.DB
	// TableName is the name of the table to retrieve documents from.
	// Default "documents".
	TableName string
	// ReturnFields limits the attributes returned from the document.
	// Default []string{"content", "vector_content"}
	ReturnFields []string
	// DocumentConverter converts retrieved raw document to eino Document, default defaultResultParser.
	DocumentConverter func(ctx context.Context, row *sql.Row) (*schema.Document, error)
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
		return nil, fmt.Errorf("[NewRetriever] embedding not provided for duckdb retriever")
	}

	if config.DB == nil {
		return nil, fmt.Errorf("[NewRetriever] duckdb database connection not provided, must pass an already opened *sql.DB instance")
	}

	if config.TableName == "" {
		config.TableName = "documents"
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
		return nil, fmt.Errorf("[duckdb retriever] embedding not provided")
	}

	vectors, err := emb.EmbedStrings(r.makeEmbeddingCtx(ctx, emb), []string{query})
	if err != nil {
		return nil, err
	}

	if len(vectors) != 1 {
		return nil, fmt.Errorf("[duckdb retriever] invalid return length of vector, got=%d, expected=1", len(vectors))
	}

	queryVector := vectors[0]

	// Build SQL query for vector search using DuckDB
	// Use list_cosine_similarity for cosine similarity search
	// Distance = 1 - similarity, so we order by similarity DESC
	// DuckDB can accept FLOAT[] which can be passed as string representation
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			vector_content,
			metadata,
			1 - list_cosine_similarity(vector_content, ?::FLOAT[]) as distance
		FROM %s
		WHERE vector_content IS NOT NULL
	`, r.config.TableName)

	// Convert []float64 to string format that DuckDB can parse
	// Format: [1.0, 2.0, 3.0]
	vectorStr := "["
	for i, v := range queryVector {
		if i > 0 {
			vectorStr += ", "
		}
		vectorStr += fmt.Sprintf("%g", v)
	}
	vectorStr += "]"

	args := []interface{}{vectorStr}

	if co.ScoreThreshold != nil {
		// For cosine similarity: similarity = 1 - distance
		// So distance threshold = 1 - scoreThreshold
		distanceThreshold := 1.0 - *co.ScoreThreshold
		sqlQuery += " AND (1 - list_cosine_similarity(vector_content, ?::FLOAT[])) <= ?"
		args = append(args, vectorStr, distanceThreshold)
	}

	sqlQuery += " ORDER BY list_cosine_similarity(vector_content, ?::FLOAT[]) DESC LIMIT ?"
	args = append(args, vectorStr, *co.TopK)

	// Execute query
	rows, err := r.config.DB.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("[duckdb retriever] search failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, content string
		var vectorContentRaw interface{}
		var metadataRaw interface{}
		var distance float64

		err := rows.Scan(&id, &content, &vectorContentRaw, &metadataRaw, &distance)
		if err != nil {
			return nil, fmt.Errorf("[duckdb retriever] failed to scan row: %w", err)
		}

		// Convert vector content from interface{} ([]interface{} for DuckDB FLOAT[])
		var vectorContent []float64
		// ... existing vector conversion code ...
		if vectorContentRaw != nil {
			if slice, ok := vectorContentRaw.([]interface{}); ok {
				vectorContent = make([]float64, len(slice))
				for i, v := range slice {
					if f, ok := v.(float64); ok {
						vectorContent[i] = f
					} else if f32, ok := v.(float32); ok {
						vectorContent[i] = float64(f32)
					}
				}
			} else if slice, ok := vectorContentRaw.([]float64); ok {
				vectorContent = slice
			}
		}

		// Create document
		doc := &schema.Document{
			ID:       id,
			Content:  content,
			MetaData: make(map[string]any),
		}

		// Set vector content if available
		if len(vectorContent) > 0 {
			doc.WithDenseVector(vectorContent)
		}

		// Parse metadata if available
		if metadataRaw != nil {
			switch v := metadataRaw.(type) {
			case map[string]interface{}:
				for k, val := range v {
					doc.MetaData[k] = val
				}
			case string:
				var metadata map[string]any
				if err := json.Unmarshal([]byte(v), &metadata); err == nil {
					for k, val := range metadata {
						doc.MetaData[k] = val
					}
				}
			case []byte:
				var metadata map[string]any
				if err := json.Unmarshal(v, &metadata); err == nil {
					for k, val := range metadata {
						doc.MetaData[k] = val
					}
				}
			}
		}

		// Set distance
		doc.MetaData[SortByDistanceAttributeName] = distance

		docs = append(docs, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("[duckdb retriever] row iteration error: %w", err)
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

const typ = "DuckDB"

func (r *Retriever) GetType() string {
	return typ
}

func (r *Retriever) IsCallbacksEnabled() bool {
	return true
}

func defaultResultParser(returnFields []string) func(ctx context.Context, row *sql.Row) (*schema.Document, error) {
	return func(ctx context.Context, row *sql.Row) (*schema.Document, error) {
		// This function is kept for compatibility but not used in the current implementation
		// The actual parsing is done directly in Retrieve method
		return nil, fmt.Errorf("defaultResultParser should not be called directly")
	}
}
