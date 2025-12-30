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

package vss

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
)

type IndexerConfig struct {
	// DB is an already opened SQLite database connection instance.
	// This indexer does not open or close the database connection.
	// The caller is responsible for managing the database connection lifecycle.
	DB *sql.DB
	// TableName is the name of the virtual table to store documents.
	// Default "documents".
	TableName string
	// VectorDimensions is the dimension of the embedding vectors.
	// This is required for creating the VSS virtual table.
	VectorDimensions int
	// DocumentToMap supports customize how to convert eino document to map.
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
		return nil, fmt.Errorf("[NewIndexer] embedding not provided for vss indexer")
	}

	if config.DB == nil {
		return nil, fmt.Errorf("[NewIndexer] sqlite database connection not provided, must pass an already opened *sql.DB instance")
	}

	if config.VectorDimensions <= 0 {
		return nil, fmt.Errorf("[NewIndexer] vector dimensions must be provided (VectorDimensions > 0)")
	}

	if config.DocumentToMap == nil {
		config.DocumentToMap = defaultDocumentToMap
	}

	if config.BatchSize == 0 {
		config.BatchSize = 10
	}

	if config.TableName == "" {
		config.TableName = "documents"
	}

	indexer := &Indexer{
		config: config,
	}

	// Initialize table schema
	if err := indexer.initSchema(ctx); err != nil {
		return nil, fmt.Errorf("[NewIndexer] failed to initialize schema: %w", err)
	}

	return indexer, nil
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

		if err := i.bulkUpsert(ctx, toStore); err != nil {
			return fmt.Errorf("[bulkStore] vss bulk upsert failed: %w", err)
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

const typ = "VSS"

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

// initSchema initializes the database table schema
func (i *Indexer) initSchema(ctx context.Context) error {
	// Create VSS virtual table for vector storage
	// SQLite VSS uses virtual tables with vss0 module
	// Format: CREATE VIRTUAL TABLE table_name USING vss0(vector(dimension), content TEXT, metadata TEXT)
	createTableSQL := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vss0(
			vector(%d),
			content TEXT,
			metadata TEXT
		)
	`, i.config.TableName, i.config.VectorDimensions)

	_, err := i.config.DB.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("[initSchema] failed to create vss virtual table: %w", err)
	}

	return nil
}

// bulkUpsert inserts or updates documents in SQLite VSS
func (i *Indexer) bulkUpsert(ctx context.Context, docs []map[string]any) error {
	if len(docs) == 0 {
		return nil
	}

	// Start a transaction for batch operations
	tx, err := i.config.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("[bulkUpsert] failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare upsert statement using INSERT OR REPLACE
	// SQLite VSS virtual table supports INSERT OR REPLACE syntax
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (vector, content, metadata)
		VALUES (?, ?, ?)
	`, i.config.TableName))
	if err != nil {
		return fmt.Errorf("[bulkUpsert] failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, doc := range docs {
		// Extract content and vector_content
		content, _ := doc[defaultReturnFieldContent].(string)

		// Get vector_content
		var vectorContent []float64
		if vec, ok := doc[defaultReturnFieldVectorContent]; ok {
			if vecSlice, ok := vec.([]float64); ok {
				vectorContent = vecSlice
			}
		}

		if len(vectorContent) == 0 {
			return fmt.Errorf("[bulkUpsert] vector content is empty")
		}

		if len(vectorContent) != i.config.VectorDimensions {
			return fmt.Errorf("[bulkUpsert] vector dimension mismatch, expected=%d, got=%d",
				i.config.VectorDimensions, len(vectorContent))
		}

		// Build metadata JSON (all fields except id, content, and vector fields)
		metadata := make(map[string]any)
		for k, v := range doc {
			if k != "id" && k != defaultReturnFieldContent && k != defaultReturnFieldVectorContent {
				metadata[k] = v
			}
		}

		// Add id to metadata for retrieval
		if id, ok := doc["id"].(string); ok {
			metadata["id"] = id
		}

		// Convert metadata to JSON
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("[bulkUpsert] failed to marshal metadata: %w", err)
		}

		// Convert vector to SQLite VSS format (BLOB)
		// SQLite VSS expects vectors as BLOB in F32 format (little-endian float32)
		vectorBlob := make([]byte, len(vectorContent)*4)
		for i, v := range vectorContent {
			binary.LittleEndian.PutUint32(vectorBlob[i*4:(i+1)*4], math.Float32bits(float32(v)))
		}

		_, err = stmt.ExecContext(ctx, vectorBlob, content, string(metadataJSON))
		if err != nil {
			return fmt.Errorf("[bulkUpsert] failed to execute statement: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("[bulkUpsert] failed to commit transaction: %w", err)
	}

	return nil
}
