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
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/embedding"
	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

const (
	// Query modes
	ModeVector   = "vector"   // Vector similarity search
	ModeFulltext = "fulltext" // Full-text search
	ModeGraph    = "graph"    // Graph-based search
	ModeHybrid   = "hybrid"   // Hybrid search combining multiple modes

	// Graph predicates
	predicateContains = "contains" // Document contains entity
	predicateRelated  = "related"  // Entities are related
)

// QueryMode represents the retrieval mode
type QueryMode string

// QueryParam defines parameters for retrieval
type QueryParam struct {
	Mode      QueryMode
	Limit     int
	Threshold float64 // Score threshold for filtering results
}

// QueryResult represents a single retrieval result
type QueryResult struct {
	ID       string
	Content  string
	Metadata map[string]any
	Score    float64
}

// LightRAG implements a LightRAG system using Cayley (graph) and DuckDB (vector + fulltext)
type LightRAG struct {
	db        *sql.DB
	graph     cayley_driver.Graph
	embedder  embedding.Embedder
	tableName string
}

// Options for creating a new LightRAG instance
type Options struct {
	// DuckDBPath is the path to the DuckDB database file
	DuckDBPath string
	// GraphPath is the path to the Cayley graph database file
	GraphPath string
	// Embedder is the embedding model for vectorization
	Embedder embedding.Embedder
	// TableName is the name of the documents table, default "documents"
	TableName string
}

// New creates a new LightRAG instance
func New(opts Options) (*LightRAG, error) {
	if opts.DuckDBPath == "" {
		return nil, fmt.Errorf("[New] DuckDBPath is required")
	}
	if opts.GraphPath == "" {
		return nil, fmt.Errorf("[New] GraphPath is required")
	}
	if opts.Embedder == nil {
		return nil, fmt.Errorf("[New] Embedder is required")
	}

	// Open DuckDB connection
	db, err := sql.Open("duckdb", opts.DuckDBPath)
	if err != nil {
		return nil, fmt.Errorf("[New] failed to open DuckDB: %w", err)
	}

	// Create graph instance
	graph, err := cayley_driver.NewGraph(opts.GraphPath)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("[New] failed to create graph: %w", err)
	}

	tableName := opts.TableName
	if tableName == "" {
		tableName = "documents"
	}

	rag := &LightRAG{
		db:        db,
		graph:     graph,
		embedder:  opts.Embedder,
		tableName: tableName,
	}

	// Initialize schema
	if err := rag.initSchema(context.Background()); err != nil {
		graph.Close()
		db.Close()
		return nil, fmt.Errorf("[New] failed to initialize schema: %w", err)
	}

	return rag, nil
}

// initSchema initializes the database schema
func (r *LightRAG) initSchema(ctx context.Context) error {
	// Create documents table with vector and fulltext support
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR PRIMARY KEY,
			content TEXT,
			vector_content FLOAT[],
			metadata JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`, r.tableName)

	_, err := r.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("[initSchema] failed to create table: %w", err)
	}

	// Create fulltext index (DuckDB supports fulltext search via FTS extension)
	// Note: DuckDB's FTS extension may need to be loaded separately
	// For now, we'll use LIKE queries or implement a simple fulltext index

	return nil
}

// InsertBatch inserts a batch of documents
func (r *LightRAG) InsertBatch(ctx context.Context, docs []map[string]any) ([]string, error) {
	if len(docs) == 0 {
		return []string{}, nil
	}

	ids := make([]string, 0, len(docs))
	texts := make([]string, 0, len(docs))
	docToVectorIdx := make(map[int]int) // Maps document index to vector index

	// Collect texts for embedding
	for i, doc := range docs {
		id, ok := doc["id"].(string)
		if !ok {
			return nil, fmt.Errorf("[InsertBatch] document %d missing id field", i)
		}
		ids = append(ids, id)

		content, ok := doc["content"].(string)
		if ok && content != "" {
			docToVectorIdx[i] = len(texts)
			texts = append(texts, content)
		}
	}

	// Embed texts
	var vectors [][]float64
	if len(texts) > 0 {
		var err error
		vectors, err = r.embedder.EmbedStrings(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("[InsertBatch] embedding failed: %w", err)
		}
		if len(vectors) != len(texts) {
			return nil, fmt.Errorf("[InsertBatch] vector count mismatch: expected %d, got %d", len(texts), len(vectors))
		}
	}

	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("[InsertBatch] failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare insert statement
	// Note: For DuckDB, we need to use ::FLOAT[] to convert string to FLOAT[]
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (id, content, vector_content, metadata)
		VALUES (?, ?, ?::FLOAT[], ?)
	`, r.tableName))
	if err != nil {
		return nil, fmt.Errorf("[InsertBatch] failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Insert documents
	for i, doc := range docs {
		id := ids[i]
		content, _ := doc["content"].(string)

		// Get vector if available
		var vector []float64
		if vectorIdx, exists := docToVectorIdx[i]; exists {
			if vectorIdx < len(vectors) {
				vector = vectors[vectorIdx]
			}
		}

		// Build metadata (all fields except id, content)
		metadata := make(map[string]any)
		for k, v := range doc {
			if k != "id" && k != "content" {
				metadata[k] = v
			}
		}

		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("[InsertBatch] failed to marshal metadata: %w", err)
		}

		// Convert vector to DuckDB-compatible format
		// DuckDB requires FLOAT[] type, but go-duckdb driver doesn't support []float64 directly
		// So we convert to string format and use CAST in SQL
		var vectorArg interface{}
		if len(vector) == 0 {
			vectorArg = nil
		} else {
			// Convert []float64 to string format that DuckDB can parse
			// Format: [1.0, 2.0, 3.0]
			vectorStr := "["
			for i, v := range vector {
				if i > 0 {
					vectorStr += ", "
				}
				vectorStr += fmt.Sprintf("%g", v)
			}
			vectorStr += "]"
			vectorArg = vectorStr
		}

		_, err = stmt.ExecContext(ctx, id, content, vectorArg, string(metadataJSON))
		if err != nil {
			return nil, fmt.Errorf("[InsertBatch] failed to execute statement: %w", err)
		}

		// Create graph relationships
		// Link document to itself (for graph traversal)
		if err := r.graph.Link(ctx, id, "is_document", id); err != nil {
			return nil, fmt.Errorf("[InsertBatch] failed to create graph link: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("[InsertBatch] failed to commit transaction: %w", err)
	}

	return ids, nil
}

// Retrieve retrieves documents based on query and parameters
func (r *LightRAG) Retrieve(ctx context.Context, query string, param QueryParam) ([]QueryResult, error) {
	mode := param.Mode
	if mode == "" {
		mode = ModeHybrid
	}

	limit := param.Limit
	if limit <= 0 {
		limit = 5
	}

	var results []QueryResult
	var err error

	switch mode {
	case ModeVector:
		results, err = r.retrieveVector(ctx, query, limit, param.Threshold)
	case ModeFulltext:
		results, err = r.retrieveFulltext(ctx, query, limit)
	case ModeGraph:
		results, err = r.retrieveGraph(ctx, query, limit)
	case ModeHybrid:
		results, err = r.retrieveHybrid(ctx, query, limit, param.Threshold)
	default:
		return nil, fmt.Errorf("[Retrieve] unknown mode: %s", mode)
	}

	if err != nil {
		return nil, err
	}

	return results, nil
}

// retrieveVector performs vector similarity search
func (r *LightRAG) retrieveVector(ctx context.Context, query string, limit int, threshold float64) ([]QueryResult, error) {
	// Embed query
	vectors, err := r.embedder.EmbedStrings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("[retrieveVector] embedding failed: %w", err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("[retrieveVector] invalid vector count: %d", len(vectors))
	}
	queryVector := vectors[0]

	// Build SQL query
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			vector_content,
			metadata,
			1 - list_cosine_similarity(vector_content, ?::FLOAT[]) as distance
		FROM %s
		WHERE vector_content IS NOT NULL
	`, r.tableName)

	args := []interface{}{queryVector}

	if threshold > 0 {
		distanceThreshold := 1.0 - threshold
		sqlQuery += " AND (1 - list_cosine_similarity(vector_content, ?::FLOAT[])) <= ?"
		args = append(args, queryVector, distanceThreshold)
	}

	sqlQuery += " ORDER BY list_cosine_similarity(vector_content, ?::FLOAT[]) DESC LIMIT ?"
	args = append(args, queryVector, limit)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("[retrieveVector] query failed: %w", err)
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var id, content string
		var vectorContent []float64
		var metadataJSON sql.NullString
		var distance float64

		if err := rows.Scan(&id, &content, &vectorContent, &metadataJSON, &distance); err != nil {
			return nil, fmt.Errorf("[retrieveVector] scan failed: %w", err)
		}

		metadata := make(map[string]any)
		if metadataJSON.Valid {
			if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err != nil {
				// Ignore JSON parse errors
			}
		}

		score := 1.0 - distance
		results = append(results, QueryResult{
			ID:       id,
			Content:  content,
			Metadata: metadata,
			Score:    score,
		})
	}

	return results, rows.Err()
}

// retrieveFulltext performs full-text search
func (r *LightRAG) retrieveFulltext(ctx context.Context, query string, limit int) ([]QueryResult, error) {
	// Use LIKE query for simple fulltext search
	// DuckDB's FTS extension would be better but requires additional setup
	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			vector_content,
			metadata
		FROM %s
		WHERE content LIKE ?
		LIMIT ?
	`, r.tableName)

	searchPattern := "%" + strings.ReplaceAll(query, "%", "\\%") + "%"
	rows, err := r.db.QueryContext(ctx, sqlQuery, searchPattern, limit)
	if err != nil {
		return nil, fmt.Errorf("[retrieveFulltext] query failed: %w", err)
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var id, content string
		var vectorContent []float64
		var metadataJSON sql.NullString

		if err := rows.Scan(&id, &content, &vectorContent, &metadataJSON); err != nil {
			return nil, fmt.Errorf("[retrieveFulltext] scan failed: %w", err)
		}

		metadata := make(map[string]any)
		if metadataJSON.Valid {
			if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err != nil {
				// Ignore JSON parse errors
			}
		}

		// Simple relevance score based on keyword matching
		score := r.calculateFulltextScore(content, query)
		results = append(results, QueryResult{
			ID:       id,
			Content:  content,
			Metadata: metadata,
			Score:    score,
		})
	}

	return results, rows.Err()
}

// retrieveGraph performs graph-based search
func (r *LightRAG) retrieveGraph(ctx context.Context, query string, limit int) ([]QueryResult, error) {
	// For graph search, we need to find documents related to query entities
	// This is a simplified implementation
	// In a real implementation, you would extract entities from the query
	// and traverse the graph to find related documents

	// For now, we'll do a simple search: find documents that contain query terms
	// and then find related documents via graph
	queryLower := strings.ToLower(query)
	words := strings.Fields(queryLower)

	// Find initial documents containing query words
	var docIDs []string
	for _, word := range words {
		sqlQuery := fmt.Sprintf(`
			SELECT id FROM %s
			WHERE LOWER(content) LIKE ?
			LIMIT ?
		`, r.tableName)
		pattern := "%" + word + "%"
		rows, err := r.db.QueryContext(ctx, sqlQuery, pattern, limit*2)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				docIDs = append(docIDs, id)
			}
		}
		rows.Close()
	}

	// Find related documents via graph
	relatedDocs := make(map[string]bool)
	for _, docID := range docIDs {
		neighbors, err := r.graph.GetNeighbors(ctx, docID, predicateRelated)
		if err == nil {
			for _, neighbor := range neighbors {
				relatedDocs[neighbor] = true
			}
		}
	}

	// Combine and retrieve
	allDocIDs := make(map[string]bool)
	for _, id := range docIDs {
		allDocIDs[id] = true
	}
	for id := range relatedDocs {
		allDocIDs[id] = true
	}

	if len(allDocIDs) == 0 {
		return []QueryResult{}, nil
	}

	// Build query with IN clause
	ids := make([]string, 0, len(allDocIDs))
	for id := range allDocIDs {
		ids = append(ids, id)
		if len(ids) >= limit {
			break
		}
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	sqlQuery := fmt.Sprintf(`
		SELECT id, content, vector_content, metadata
		FROM %s
		WHERE id IN (%s)
		LIMIT ?
	`, r.tableName, strings.Join(placeholders, ","))
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("[retrieveGraph] query failed: %w", err)
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var id, content string
		var vectorContent []float64
		var metadataJSON sql.NullString

		if err := rows.Scan(&id, &content, &vectorContent, &metadataJSON); err != nil {
			return nil, fmt.Errorf("[retrieveGraph] scan failed: %w", err)
		}

		metadata := make(map[string]any)
		if metadataJSON.Valid {
			if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err != nil {
				// Ignore JSON parse errors
			}
		}

		results = append(results, QueryResult{
			ID:       id,
			Content:  content,
			Metadata: metadata,
			Score:    0.5, // Default score for graph results
		})
	}

	return results, rows.Err()
}

// retrieveHybrid performs hybrid search combining vector and fulltext
func (r *LightRAG) retrieveHybrid(ctx context.Context, query string, limit int, threshold float64) ([]QueryResult, error) {
	// Get results from both vector and fulltext search
	vectorResults, _ := r.retrieveVector(ctx, query, limit*2, threshold)
	fulltextResults, _ := r.retrieveFulltext(ctx, query, limit*2)

	// Combine and deduplicate
	resultMap := make(map[string]*QueryResult)
	for i := range vectorResults {
		result := &vectorResults[i]
		if existing, exists := resultMap[result.ID]; exists {
			// Combine scores (weighted average)
			existing.Score = (existing.Score*0.6 + result.Score*0.4)
		} else {
			result.Score = result.Score * 0.6 // Weight vector results
			resultMap[result.ID] = result
		}
	}

	for i := range fulltextResults {
		result := &fulltextResults[i]
		if existing, exists := resultMap[result.ID]; exists {
			// Combine scores
			existing.Score = (existing.Score + result.Score*0.4)
		} else {
			result.Score = result.Score * 0.4 // Weight fulltext results
			resultMap[result.ID] = result
		}
	}

	// Convert to slice and sort by score
	results := make([]QueryResult, 0, len(resultMap))
	for _, result := range resultMap {
		results = append(results, *result)
	}

	// Simple sort by score (descending)
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Score < results[j].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Limit results
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// calculateFulltextScore calculates a simple relevance score for fulltext search
func (r *LightRAG) calculateFulltextScore(content, query string) float64 {
	contentLower := strings.ToLower(content)
	queryLower := strings.ToLower(query)
	words := strings.Fields(queryLower)

	if len(words) == 0 {
		return 0.0
	}

	matches := 0
	for _, word := range words {
		if strings.Contains(contentLower, word) {
			matches++
		}
	}

	return float64(matches) / float64(len(words))
}

// Close closes the LightRAG instance and releases resources
func (r *LightRAG) Close() error {
	var errs []error
	if r.graph != nil {
		if err := r.graph.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.db != nil {
		if err := r.db.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing LightRAG: %v", errs)
	}
	return nil
}
