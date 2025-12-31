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
	"time"

	"github.com/cloudwego/eino/components/embedding"
	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	"github.com/sirupsen/logrus"
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

	// Default batch size for embedding API calls
	defaultEmbeddingBatchSize = 10
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
	// WorkingDir is the working directory, used as base directory
	WorkingDir string
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
	if opts.WorkingDir == "" {
		db.Close()
		return nil, fmt.Errorf("[New] WorkingDir is required")
	}
	graph, err := cayley_driver.NewGraphWithPrefix(opts.WorkingDir, opts.GraphPath, "")
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
	if r == nil {
		return fmt.Errorf("[initSchema] LightRAG instance is nil")
	}
	if r.db == nil {
		return fmt.Errorf("[initSchema] database connection is not available")
	}
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
	if r == nil {
		return nil, fmt.Errorf("[InsertBatch] LightRAG instance is nil")
	}
	if len(docs) == 0 {
		return []string{}, nil
	}

	// Process documents in batches to respect API batch size limits
	batchSize := defaultEmbeddingBatchSize
	allIds := make([]string, 0, len(docs))

	for batchStart := 0; batchStart < len(docs); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(docs) {
			batchEnd = len(docs)
		}
		batchDocs := docs[batchStart:batchEnd]

		ids := make([]string, 0, len(batchDocs))
		texts := make([]string, 0, len(batchDocs))
		docToVectorIdx := make(map[int]int)      // Maps document index to vector index
		vectorIdxToDocID := make(map[int]string) // Maps vector index to document ID

		// Collect texts for embedding
		for i, doc := range batchDocs {
			if doc == nil {
				return nil, fmt.Errorf("[InsertBatch] document at index %d is nil", batchStart+i)
			}
			id, ok := doc["id"].(string)
			if !ok {
				return nil, fmt.Errorf("[InsertBatch] document %d missing id field", batchStart+i)
			}
			ids = append(ids, id)

			content, ok := doc["content"].(string)
			if ok && content != "" {
				vectorIdx := len(texts)
				docToVectorIdx[i] = vectorIdx
				vectorIdxToDocID[vectorIdx] = id
				texts = append(texts, content)
			}
		}

		// Embed texts in batches
		var vectors [][]float64
		if len(texts) > 0 {
			if r.embedder == nil {
				return nil, fmt.Errorf("[InsertBatch] embedder is not available")
			}

			// 记录 embedding 开始日志
			logrus.WithFields(logrus.Fields{
				"batch":        fmt.Sprintf("%d-%d", batchStart, batchEnd-1),
				"total_docs":   len(docs),
				"batch_size":   len(batchDocs),
				"text_count":   len(texts),
				"batch_number": (batchStart / batchSize) + 1,
			}).Info("[Embedding] 开始进行 embedding 处理")

			// 打印每个文本的预览信息
			for i, text := range texts {
				previewLen := 100
				preview := text
				if len(text) > previewLen {
					preview = text[:previewLen] + "..."
				}
				docID := vectorIdxToDocID[i]
				logrus.WithFields(logrus.Fields{
					"batch":       fmt.Sprintf("%d-%d", batchStart, batchEnd-1),
					"text_index":  i,
					"text_id":     docID,
					"text_length": len(text),
					"preview":     preview,
				}).Debug("[Embedding] 准备 embedding 的文本")
			}

			// 记录开始时间
			embedStartTime := time.Now()
			var err error
			vectors, err = r.embedder.EmbedStrings(ctx, texts)
			embedDuration := time.Since(embedStartTime)

			if err != nil {
				logrus.WithFields(logrus.Fields{
					"batch":      fmt.Sprintf("%d-%d", batchStart, batchEnd-1),
					"text_count": len(texts),
					"error":      err,
				}).Error("[Embedding] embedding 失败")
				return nil, fmt.Errorf("[InsertBatch] embedding failed: %w", err)
			}

			if len(vectors) != len(texts) {
				logrus.WithFields(logrus.Fields{
					"batch":          fmt.Sprintf("%d-%d", batchStart, batchEnd-1),
					"expected_count": len(texts),
					"actual_count":   len(vectors),
				}).Error("[Embedding] 向量数量不匹配")
				return nil, fmt.Errorf("[InsertBatch] vector count mismatch: expected %d, got %d", len(texts), len(vectors))
			}

			// 记录 embedding 完成日志
			var vectorDim int
			if len(vectors) > 0 {
				vectorDim = len(vectors[0])
			}
			logrus.WithFields(logrus.Fields{
				"batch":        fmt.Sprintf("%d-%d", batchStart, batchEnd-1),
				"text_count":   len(texts),
				"vector_count": len(vectors),
				"vector_dim":   vectorDim,
				"duration_ms":  embedDuration.Milliseconds(),
				"duration_sec": embedDuration.Seconds(),
				"avg_time_ms":  float64(embedDuration.Milliseconds()) / float64(len(texts)),
			}).Info("[Embedding] embedding 处理完成")

			// 打印每个向量的基本信息
			for i, vector := range vectors {
				docID := vectorIdxToDocID[i]
				logrus.WithFields(logrus.Fields{
					"batch":        fmt.Sprintf("%d-%d", batchStart, batchEnd-1),
					"vector_index": i,
					"text_id":      docID,
					"vector_dim":   len(vector),
					"first_3_vals": func() []float64 {
						if len(vector) >= 3 {
							return vector[:3]
						}
						return vector
					}(),
				}).Debug("[Embedding] 生成的向量信息")
			}
		}

		// Start transaction
		if r.db == nil {
			return nil, fmt.Errorf("[InsertBatch] database connection is not available")
		}
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

		// Insert documents
		for i, doc := range batchDocs {
			if doc == nil {
				stmt.Close()
				return nil, fmt.Errorf("[InsertBatch] document at index %d is nil", batchStart+i)
			}
			if i >= len(ids) {
				stmt.Close()
				return nil, fmt.Errorf("[InsertBatch] index %d out of range for ids array (length: %d)", i, len(ids))
			}
			id := ids[i]
			content, _ := doc["content"].(string)

			// Get vector if available
			var vector []float64
			if docToVectorIdx != nil {
				if vectorIdx, exists := docToVectorIdx[i]; exists {
					if vectorIdx < len(vectors) {
						vector = vectors[vectorIdx]
					}
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
				stmt.Close()
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
				stmt.Close()
				return nil, fmt.Errorf("[InsertBatch] failed to execute statement: %w", err)
			}

			// Create graph relationships
			// Link document to itself (for graph traversal)
			if r.graph != nil {
				if err := r.graph.Link(ctx, id, "is_document", id); err != nil {
					stmt.Close()
					return nil, fmt.Errorf("[InsertBatch] failed to create graph link: %w", err)
				}
			}
		}

		stmt.Close()

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("[InsertBatch] failed to commit transaction: %w", err)
		}

		allIds = append(allIds, ids...)
	}

	return allIds, nil
}

// Retrieve retrieves documents based on query and parameters
func (r *LightRAG) Retrieve(ctx context.Context, query string, param QueryParam) ([]QueryResult, error) {
	if r == nil {
		return nil, fmt.Errorf("[Retrieve] LightRAG instance is nil")
	}
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
	if r == nil {
		return nil, fmt.Errorf("[retrieveVector] LightRAG instance is nil")
	}
	if r.embedder == nil {
		return nil, fmt.Errorf("[retrieveVector] embedder is not available")
	}
	if r.db == nil {
		return nil, fmt.Errorf("[retrieveVector] database connection is not available")
	}
	// Embed query
	logrus.WithFields(logrus.Fields{
		"query":        query,
		"query_length": len(query),
		"limit":        limit,
		"threshold":    threshold,
	}).Info("[Embedding] 开始对查询进行 embedding")

	embedStartTime := time.Now()
	vectors, err := r.embedder.EmbedStrings(ctx, []string{query})
	embedDuration := time.Since(embedStartTime)

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"query": query,
			"error": err,
		}).Error("[Embedding] 查询 embedding 失败")
		return nil, fmt.Errorf("[retrieveVector] embedding failed: %w", err)
	}
	if len(vectors) != 1 {
		logrus.WithFields(logrus.Fields{
			"query":          query,
			"expected_count": 1,
			"actual_count":   len(vectors),
		}).Error("[Embedding] 查询向量数量不匹配")
		return nil, fmt.Errorf("[retrieveVector] invalid vector count: %d", len(vectors))
	}
	queryVector := vectors[0]

	logrus.WithFields(logrus.Fields{
		"query":        query,
		"vector_dim":   len(queryVector),
		"duration_ms":  embedDuration.Milliseconds(),
		"duration_sec": embedDuration.Seconds(),
		"first_3_vals": func() []float64 {
			if len(queryVector) >= 3 {
				return queryVector[:3]
			}
			return queryVector
		}(),
	}).Info("[Embedding] 查询 embedding 完成")

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

	if threshold > 0 {
		distanceThreshold := 1.0 - threshold
		sqlQuery += " AND (1 - list_cosine_similarity(vector_content, ?::FLOAT[])) <= ?"
		args = append(args, vectorStr, distanceThreshold)
	}

	sqlQuery += " ORDER BY list_cosine_similarity(vector_content, ?::FLOAT[]) DESC LIMIT ?"
	args = append(args, vectorStr, limit)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("[retrieveVector] query failed: %w", err)
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var id, content string
		var vectorContentRaw interface{}
		var metadataRaw interface{}
		var distance float64

		if err := rows.Scan(&id, &content, &vectorContentRaw, &metadataRaw, &distance); err != nil {
			return nil, fmt.Errorf("[retrieveVector] scan failed: %w", err)
		}

		// Convert vector content from interface{} ([]interface{} for DuckDB FLOAT[])
		var vectorContent []float64
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

		metadata := make(map[string]any)
		if metadataRaw != nil {
			// DuckDB may return JSON as map[string]interface{} or as JSON string
			switch v := metadataRaw.(type) {
			case map[string]interface{}:
				metadata = v
			case string:
				if err := json.Unmarshal([]byte(v), &metadata); err != nil {
					// Ignore JSON parse errors
				}
			case []byte:
				if err := json.Unmarshal(v, &metadata); err != nil {
					// Ignore JSON parse errors
				}
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
	if r == nil {
		return nil, fmt.Errorf("[retrieveFulltext] LightRAG instance is nil")
	}
	if r.db == nil {
		return nil, fmt.Errorf("[retrieveFulltext] database connection is not available")
	}
	// Use LIKE query for simple fulltext search
	// Split query into words and search for documents containing all words
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return []QueryResult{}, nil
	}

	whereClauses := make([]string, len(words))
	args := make([]interface{}, len(words))
	for i, word := range words {
		whereClauses[i] = "LOWER(content) LIKE ?"
		args[i] = "%" + strings.ReplaceAll(word, "%", "\\%") + "%"
	}

	sqlQuery := fmt.Sprintf(`
		SELECT 
			id,
			content,
			metadata
		FROM %s
		WHERE %s
		LIMIT ?
	`, r.tableName, strings.Join(whereClauses, " AND "))

	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("[retrieveFulltext] query failed: %w", err)
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var id, content string
		var metadataRaw interface{}

		if err := rows.Scan(&id, &content, &metadataRaw); err != nil {
			return nil, fmt.Errorf("[retrieveFulltext] scan failed: %w", err)
		}

		metadata := make(map[string]any)
		if metadataRaw != nil {
			// DuckDB may return JSON as map[string]interface{} or as JSON string
			switch v := metadataRaw.(type) {
			case map[string]interface{}:
				metadata = v
			case string:
				if err := json.Unmarshal([]byte(v), &metadata); err != nil {
					// Ignore JSON parse errors
				}
			case []byte:
				if err := json.Unmarshal(v, &metadata); err != nil {
					// Ignore JSON parse errors
				}
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
	if r == nil {
		return nil, fmt.Errorf("[retrieveGraph] LightRAG instance is nil")
	}
	if r.db == nil {
		return nil, fmt.Errorf("[retrieveGraph] database connection is not available")
	}
	if r.graph == nil {
		return nil, fmt.Errorf("[retrieveGraph] graph database is not available")
	}
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
	if r.graph != nil {
		for _, docID := range docIDs {
			neighbors, err := r.graph.GetNeighbors(ctx, docID, predicateRelated)
			if err == nil {
				for _, neighbor := range neighbors {
					relatedDocs[neighbor] = true
				}
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
		SELECT id, content, metadata
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
		var metadataRaw interface{}

		if err := rows.Scan(&id, &content, &metadataRaw); err != nil {
			return nil, fmt.Errorf("[retrieveGraph] scan failed: %w", err)
		}

		metadata := make(map[string]any)
		if metadataRaw != nil {
			// DuckDB may return JSON as map[string]interface{} or as JSON string
			switch v := metadataRaw.(type) {
			case map[string]interface{}:
				metadata = v
			case string:
				if err := json.Unmarshal([]byte(v), &metadata); err != nil {
					// Ignore JSON parse errors
				}
			case []byte:
				if err := json.Unmarshal(v, &metadata); err != nil {
					// Ignore JSON parse errors
				}
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
	if r == nil {
		return nil, fmt.Errorf("[retrieveHybrid] LightRAG instance is nil")
	}
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
