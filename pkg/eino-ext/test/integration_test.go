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

package test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
	"github.com/smartystreets/goconvey/convey"

	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/document/transformer/splitter/tfidf"
	duckdbindexer "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/indexer/dockdb"
	lightragindexer "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/indexer/lightrag"
	duckdbretriever "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/retriever/dockdb"
	lightragretriever "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/retriever/lightrag"
)

type simpleEmbedder struct {
	dims int
}

func (e *simpleEmbedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	vectors := make([][]float64, len(texts))
	for i, text := range texts {
		vectors[i] = make([]float64, e.dims)
		lowerText := strings.ToLower(text)
		if strings.Contains(lowerText, "eino") {
			vectors[i][0] = 1.0
		}
		if strings.Contains(lowerText, "duckdb") {
			vectors[i][1] = 1.0
		}
		if strings.Contains(lowerText, "framework") {
			vectors[i][2] = 1.0
		}
		// Add some noise to avoid all-zero vectors
		vectors[i][3] = 0.01
	}
	return vectors, nil
}

func TestDuckDBFullFlow(t *testing.T) {
	convey.Convey("Test DuckDB Full RAG Flow", t, func() {
		ctx := context.Background()
		workingDir := "./test_duckdb_full_flow"
		os.RemoveAll(workingDir)
		if err := os.MkdirAll(workingDir, 0755); err != nil {
			t.Fatalf("Failed to create working directory: %v", err)
		}
		defer os.RemoveAll(workingDir)

		dbPath := filepath.Join(workingDir, "test.db")
		db, err := sql.Open("duckdb", dbPath)
		convey.So(err, convey.ShouldBeNil)
		defer db.Close()

		// 1. Splitter
		splitter, err := tfidf.NewTFIDFSplitter(ctx, &tfidf.Config{
			SimilarityThreshold:  0.1,
			MaxChunkSize:         1000, // 从 100 句子改为 1000 字符
			MaxSentencesPerChunk: 100,  // 保持原来的句子数限制
			IDGenerator: func(ctx context.Context, originalID string, splitIndex int) string {
				return fmt.Sprintf("%s_%d", originalID, splitIndex)
			},
		})
		convey.So(err, convey.ShouldBeNil)

		doc := &schema.Document{
			ID:      "doc1",
			Content: "Eino is a framework for building LLM applications. It is developed by CloudWeGo. DuckDB is an in-process SQL OLAP database management system.",
			MetaData: map[string]any{
				"author": "CloudWeGo",
				"type":   "framework",
			},
		}

		splitDocs, err := splitter.Transform(ctx, []*schema.Document{doc})
		convey.So(err, convey.ShouldBeNil)
		convey.So(len(splitDocs), convey.ShouldBeGreaterThan, 0)

		// 2. Indexer
		embedder := &simpleEmbedder{dims: 8}
		idx, err := duckdbindexer.NewIndexer(ctx, &duckdbindexer.IndexerConfig{
			DB:        db,
			TableName: "test_docs",
			Embedding: embedder,
		})
		convey.So(err, convey.ShouldBeNil)

		ids, err := idx.Store(ctx, splitDocs)
		convey.So(err, convey.ShouldBeNil)
		convey.So(len(ids), convey.ShouldEqual, len(splitDocs))

		// 3. Retriever
		ret, err := duckdbretriever.NewRetriever(ctx, &duckdbretriever.RetrieverConfig{
			DB:        db,
			TableName: "test_docs",
			Embedding: embedder,
			TopK:      1,
		})
		convey.So(err, convey.ShouldBeNil)

		results, err := ret.Retrieve(ctx, "Eino framework")
		convey.So(err, convey.ShouldBeNil)
		convey.So(len(results), convey.ShouldEqual, 1)
		convey.So(results[0].Content, convey.ShouldContainSubstring, "Eino")
		convey.So(results[0].MetaData["author"], convey.ShouldEqual, "CloudWeGo")
		convey.So(results[0].MetaData["type"], convey.ShouldEqual, "framework")
		// DenseVector is a method in some versions of eino schema, or a field in others.
		// Based on the error, it's a method.
		convey.So(len(results[0].DenseVector()), convey.ShouldEqual, 8)
	})
}

func TestLightRAGFullFlow(t *testing.T) {
	convey.Convey("Test LightRAG Full RAG Flow", t, func() {
		ctx := context.Background()
		workingDir := "./test_lightrag_full_flow"
		os.RemoveAll(workingDir)
		if err := os.MkdirAll(workingDir, 0755); err != nil {
			t.Fatalf("Failed to create working directory: %v", err)
		}
		defer os.RemoveAll(workingDir)

		duckdbPath := filepath.Join(workingDir, "duckdb.db")
		graphPath := filepath.Join(workingDir, "graph.db")
		embedder := &simpleEmbedder{dims: 8}

		rag, err := lightragindexer.New(lightragindexer.Options{
			DuckDBPath: duckdbPath,
			GraphPath:  graphPath,
			Embedder:   embedder,
		})
		convey.So(err, convey.ShouldBeNil)
		defer rag.Close()

		// 1. Indexer
		idx, err := lightragindexer.NewIndexer(ctx, &lightragindexer.IndexerConfig{
			LightRAG: rag,
		})
		convey.So(err, convey.ShouldBeNil)

		doc := &schema.Document{
			ID:      "lightrag_doc1",
			Content: "LightRAG combines graph and vector search for better retrieval performance.",
		}
		ids, err := idx.Store(ctx, []*schema.Document{doc})
		convey.So(err, convey.ShouldBeNil)
		convey.So(len(ids), convey.ShouldEqual, 1)

		// 2. Retriever
		ret, err := lightragretriever.NewRetriever(ctx, &lightragretriever.RetrieverConfig{
			LightRAG: rag,
			TopK:     1,
			Mode:     lightragindexer.ModeFulltext,
		})
		convey.So(err, convey.ShouldBeNil)

		results, err := ret.Retrieve(ctx, "LightRAG graph")
		convey.So(err, convey.ShouldBeNil)
		convey.So(len(results), convey.ShouldEqual, 1)
		convey.So(results[0].Content, convey.ShouldContainSubstring, "LightRAG")
	})
}
