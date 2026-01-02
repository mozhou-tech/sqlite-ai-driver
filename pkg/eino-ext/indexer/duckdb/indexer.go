package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
)

const (
	defaultReturnFieldContent       = "content"
	defaultReturnFieldVectorContent = "vector_content"
)

type IndexerConfig struct {
	// DB is an already opened SQLite database connection instance.
	// This indexer does not open or close the database connection.
	// The caller is responsible for managing the database connection lifecycle.
	DB *sql.DB
	// TableName is the name of the table to store documents.
	// Default "documents".
	TableName string
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
		return nil, fmt.Errorf("[NewIndexer] embedding not provided for sqlite indexer")
	}

	if config.DB == nil {
		return nil, fmt.Errorf("[NewIndexer] sqlite database connection not provided, must pass an already opened *sql.DB instance")
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
			return fmt.Errorf("[bulkStore] sqlite bulk upsert failed: %w", err)
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

const typ = "SQLite"

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
	// Create table for SQLite vector storage
	// SQLite supports FLOAT[] type for vectors
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR PRIMARY KEY,
			content TEXT,
			vector_content FLOAT[],
			metadata JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`, i.config.TableName)

	_, err := i.config.DB.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("[initSchema] failed to create table: %w", err)
	}

	return nil
}

// bulkUpsert inserts or updates documents in SQLite
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
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (id, content, vector_content, metadata)
		VALUES (?, ?, ?, ?)
	`, i.config.TableName))
	if err != nil {
		return fmt.Errorf("[bulkUpsert] failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, doc := range docs {
		// Extract id, content and vector_content
		id, _ := doc["id"].(string)
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

		// Convert vector to SQLite format (string representation)
		// Format: [1.0, 2.0, 3.0]
		vectorStr := "["
		for i, v := range vectorContent {
			if i > 0 {
				vectorStr += ", "
			}
			vectorStr += fmt.Sprintf("%g", v)
		}
		vectorStr += "]"

		// Build metadata JSON (all fields except id, content, and vector fields)
		metadata := make(map[string]any)
		for k, v := range doc {
			if k != "id" && k != defaultReturnFieldContent && k != defaultReturnFieldVectorContent {
				metadata[k] = v
			}
		}

		// Convert metadata to JSON
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("[bulkUpsert] failed to marshal metadata: %w", err)
		}

		_, err = stmt.ExecContext(ctx, id, content, vectorStr, string(metadataJSON))
		if err != nil {
			return fmt.Errorf("[bulkUpsert] failed to execute statement: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("[bulkUpsert] failed to commit transaction: %w", err)
	}

	return nil
}
