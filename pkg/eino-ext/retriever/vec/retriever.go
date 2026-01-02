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
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/vecstore"
)

type RetrieverConfig struct {
	// VecStore is a vecstore instance used as the underlying storage.
	// This retriever uses vecstore as the backend storage.
	VecStore *vecstore.VecStore
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
	config    *RetrieverConfig
	db        *sql.DB
	tableName string
}

func NewRetriever(ctx context.Context, config *RetrieverConfig) (*Retriever, error) {
	if config.Embedding == nil {
		return nil, fmt.Errorf("[NewRetriever] embedding not provided for sqlite retriever")
	}

	if config.VecStore == nil {
		return nil, fmt.Errorf("[NewRetriever] vecstore instance not provided, must pass a *vecstore.VecStore instance")
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

	// Initialize vecstore if not already initialized
	if err := config.VecStore.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("[NewRetriever] failed to initialize vecstore: %w", err)
	}

	// Access vecstore's internal fields via public methods
	db, tableName, err := getVecStoreInternalsForRetriever(config.VecStore)
	if err != nil {
		return nil, fmt.Errorf("[NewRetriever] failed to access vecstore internals: %w", err)
	}

	return &Retriever{
		config:    config,
		db:        db,
		tableName: tableName,
	}, nil
}

// getVecStoreInternalsForRetriever gets vecstore's internal fields via public methods
func getVecStoreInternalsForRetriever(vs *vecstore.VecStore) (*sql.DB, string, error) {
	db := vs.GetDB()
	if db == nil {
		return nil, "", fmt.Errorf("vecstore db is nil")
	}
	tableName := vs.GetTableName()
	return db, tableName, nil
}

func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) (docs []*schema.Document, err error) {
	co := retriever.GetCommonOptions(&retriever.Options{
		TopK:      &r.config.TopK,
		Embedding: r.config.Embedding,
	}, opts...)
	io := retriever.GetImplSpecificOptions(&implOptions{}, opts...)

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
		return nil, fmt.Errorf("[sqlite retriever] embedding not provided")
	}

	// Generate query vector using eino embedder
	vectors, err := emb.EmbedStrings(r.makeEmbeddingCtx(ctx, emb), []string{query})
	if err != nil {
		return nil, fmt.Errorf("[sqlite retriever] failed to embed query: %w", err)
	}

	if len(vectors) != 1 {
		return nil, fmt.Errorf("[sqlite retriever] invalid return length of vector, got=%d, expected=1", len(vectors))
	}

	queryVector := vectors[0]

	// Convert []float64 to string format that SQLite can parse
	// Format: [1.0, 2.0, 3.0]
	vectorStr := "["
	for i, v := range queryVector {
		if i > 0 {
			vectorStr += ", "
		}
		vectorStr += fmt.Sprintf("%g", v)
	}
	vectorStr += "]"

	// Build SQL query for vector search using vecstore's table structure
	// Use list_cosine_similarity for cosine similarity search
	// vecstore stores vectors in 'embedding' column and content in 'content' column (as JSON)
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata,
			list_cosine_similarity(embedding, ?) as similarity
		FROM %s
		WHERE embedding IS NOT NULL AND embedding_status = 'completed'
	`, r.tableName)

	args := []interface{}{vectorStr}

	// Add metadata filters if provided
	if io.MetadataFilter != nil && len(io.MetadataFilter) > 0 {
		for key, value := range io.MetadataFilter {
			// Use json_extract_path_text similar to vecstore's implementation
			escapedKey := key // Simple escaping, could be improved
			condition := fmt.Sprintf(
				"(json_extract_path_text(COALESCE(metadata, '{}'), '%s') = ? OR json_extract_path_text(content, '$.%s') = ?)",
				escapedKey, escapedKey,
			)
			sqlQuery += " AND (" + condition + ")"

			// Convert value to string for comparison
			var valueStr string
			switch val := value.(type) {
			case string:
				valueStr = val
			case []byte:
				valueStr = string(val)
			default:
				valueBytes, _ := json.Marshal(val)
				valueStr = string(valueBytes)
				if len(valueStr) >= 2 && valueStr[0] == '"' && valueStr[len(valueStr)-1] == '"' {
					valueStr = valueStr[1 : len(valueStr)-1]
				}
			}
			args = append(args, valueStr, valueStr)
		}
	}

	// Add score threshold if provided
	if co.ScoreThreshold != nil {
		sqlQuery += " AND list_cosine_similarity(embedding, ?) >= ?"
		args = append(args, vectorStr, *co.ScoreThreshold)
	}

	sqlQuery += " ORDER BY list_cosine_similarity(embedding, ?) DESC LIMIT ?"
	args = append(args, vectorStr, *co.TopK)

	// Execute query using vecstore's database connection
	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("[sqlite retriever] search failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, content string
		var metadataRaw interface{}
		var similarity float64

		err := rows.Scan(&id, &content, &metadataRaw, &similarity)
		if err != nil {
			return nil, fmt.Errorf("[sqlite retriever] failed to scan row: %w", err)
		}

		// Parse content JSON (vecstore stores all document data in content field as JSON)
		var contentDoc map[string]any
		if err := json.Unmarshal([]byte(content), &contentDoc); err != nil {
			// If content is not JSON, treat it as plain text
			contentDoc = map[string]any{"id": id, "content": content}
		}

		// Extract text content from contentDoc
		textContent := ""
		if text, ok := contentDoc["content"].(string); ok {
			textContent = text
		}

		// Create document
		doc := &schema.Document{
			ID:       id,
			Content:  textContent,
			MetaData: make(map[string]any),
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

		// Also add fields from contentDoc to metadata (excluding id and content)
		for k, v := range contentDoc {
			if k != "id" && k != "content" {
				doc.MetaData[k] = v
			}
		}

		// Set distance (vecstore returns similarity, convert to distance)
		// Distance = 1 - similarity
		distance := 1.0 - similarity
		doc.MetaData[SortByDistanceAttributeName] = distance

		docs = append(docs, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("[sqlite retriever] row iteration error: %w", err)
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

const typ = "SQLite"

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
