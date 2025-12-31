package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphstore"
	"github.com/sirupsen/logrus"
)

// LLM defines the interface for language models used to extract keywords from queries.
type LLM interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// QueryKeywords contains low-level and high-level keywords extracted from a query.
type QueryKeywords struct {
	LowLevel  []string `json:"low_level"`
	HighLevel []string `json:"high_level"`
}

// DocumentGetter is an optional function to retrieve document content by ID.
// If provided, the retriever will use it to fetch document content.
// If not provided, the retriever will return documents with only ID and metadata.
type DocumentGetter func(ctx context.Context, docID string) (*schema.Document, error)

// RetrieverConfig defines the configuration for the Graph retriever.
type RetrieverConfig struct {
	// Graph is the GraphStore instance to use for retrieval.
	Graph     *graphstore.GraphStore
	TableName string
	// LLM is the language model to use for extracting keywords from queries.
	LLM LLM
	// DocumentGetter is an optional function to retrieve document content by ID.
	// If not provided, documents will only contain ID and metadata.
	DocumentGetter DocumentGetter
	// TopK limits the number of results returned, default 5.
	TopK int
	// MaxDepth limits the depth of graph traversal when searching for related entities, default 2.
	MaxDepth int
	// Transformer optionally transforms documents after retrieval (e.g. splitting).
	Transformer document.Transformer
}

// Retriever implements the Eino retriever.Retriever interface for Graph-based retrieval.
type Retriever struct {
	config *RetrieverConfig
}

// NewRetriever creates a new Graph retriever.
func NewRetriever(ctx context.Context, config *RetrieverConfig) (*Retriever, error) {
	if config.Graph == nil {
		return nil, fmt.Errorf("[NewRetriever] graph instance not provided")
	}

	if config.LLM == nil {
		return nil, fmt.Errorf("[NewRetriever] LLM instance not provided")
	}

	if config.TopK <= 0 {
		config.TopK = 5
	}

	if config.MaxDepth <= 0 {
		config.MaxDepth = 2
	}

	// Ensure GraphStore is initialized
	if err := config.Graph.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("[NewRetriever] failed to initialize graph store: %w", err)
	}

	return &Retriever{
		config: config,
	}, nil
}

// Retrieve retrieves relevant documents from the graph database.
func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) (docs []*schema.Document, err error) {
	if r == nil {
		return nil, fmt.Errorf("[Retrieve] Retriever instance is nil")
	}
	if r.config == nil {
		return nil, fmt.Errorf("[Retrieve] config is nil")
	}

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

	// Extract keywords from query
	keywords, err := r.extractQueryKeywords(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("[Retrieve] failed to extract keywords: %w", err)
	}

	// Combine low-level and high-level keywords
	allKeywords := append(keywords.LowLevel, keywords.HighLevel...)
	if len(allKeywords) == 0 {
		logrus.Warn("No keywords extracted from query, returning empty results")
		callbacks.OnEnd(ctx, &retriever.CallbackOutput{Docs: []*schema.Document{}})
		return []*schema.Document{}, nil
	}

	logrus.WithFields(logrus.Fields{
		"query":      query,
		"low_level":  keywords.LowLevel,
		"high_level": keywords.HighLevel,
	}).Info("Extracted keywords from query")

	// Find documents through graph traversal
	docIDMap := make(map[string]bool)
	entityMap := make(map[string]bool)

	// For each keyword, find related entities and then find documents
	for _, keyword := range allKeywords {
		// Find entities that match the keyword (exact match or through graph traversal)
		entities, err := r.findRelatedEntities(ctx, keyword, r.config.MaxDepth)
		if err != nil {
			logrus.WithError(err).WithField("keyword", keyword).Warn("Failed to find related entities")
			continue
		}

		for _, entity := range entities {
			if entityMap[entity] {
				continue
			}
			entityMap[entity] = true

			// Find documents that contain this entity
			docIDs, err := r.config.Graph.GetNeighbors(ctx, entity, "APPEARS_IN")
			if err != nil {
				logrus.WithError(err).WithField("entity", entity).Warn("Failed to get document neighbors")
				continue
			}

			for _, docID := range docIDs {
				docIDMap[docID] = true
			}
		}
	}

	if len(docIDMap) == 0 {
		logrus.Info("No documents found through graph traversal")
		callbacks.OnEnd(ctx, &retriever.CallbackOutput{Docs: []*schema.Document{}})
		return []*schema.Document{}, nil
	}

	// Convert document IDs to documents
	docs = make([]*schema.Document, 0, len(docIDMap))
	count := 0
	for docID := range docIDMap {
		if count >= *co.TopK {
			break
		}

		var doc *schema.Document
		if r.config.DocumentGetter != nil {
			// Try to get full document content
			retrievedDoc, err := r.config.DocumentGetter(ctx, docID)
			if err != nil {
				logrus.WithError(err).WithField("doc_id", docID).Warn("Failed to get document content, using ID only")
				doc = &schema.Document{
					ID:       docID,
					Content:  "",
					MetaData: make(map[string]any),
				}
			} else {
				doc = retrievedDoc
			}
		} else {
			// Create document with ID only
			doc = &schema.Document{
				ID:       docID,
				Content:  "",
				MetaData: make(map[string]any),
			}
		}

		// Add metadata
		if doc.MetaData == nil {
			doc.MetaData = make(map[string]any)
		}
		doc.MetaData["score"] = 1.0 // Graph-based retrieval uses uniform scoring
		doc.MetaData["retrieval_method"] = "graph"

		docs = append(docs, doc)
		count++
	}

	// Apply transformer if provided
	if r.config.Transformer != nil {
		docs, err = r.config.Transformer.Transform(ctx, docs)
		if err != nil {
			return nil, fmt.Errorf("[Retrieve] failed to transform documents: %w", err)
		}
	}

	callbacks.OnEnd(ctx, &retriever.CallbackOutput{Docs: docs})

	return docs, nil
}

// extractQueryKeywords extracts keywords from a query using LLM.
func (r *Retriever) extractQueryKeywords(ctx context.Context, query string) (*QueryKeywords, error) {
	if r.config.LLM == nil {
		return nil, fmt.Errorf("[extractQueryKeywords] LLM is not available")
	}

	// Get query entity extraction prompt
	promptStr, err := getQueryEntityPrompt(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("[extractQueryKeywords] failed to get query entity prompt: %w", err)
	}

	// Call LLM to extract keywords
	response, err := r.config.LLM.Complete(ctx, promptStr)
	if err != nil {
		return nil, fmt.Errorf("[extractQueryKeywords] failed to complete LLM request: %w", err)
	}

	// Parse JSON response
	jsonStr := response
	idxStart := strings.Index(jsonStr, "{")
	idxEnd := strings.LastIndex(jsonStr, "}")
	if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
		return nil, fmt.Errorf("[extractQueryKeywords] no JSON object found in response: %s", response)
	}
	jsonStr = jsonStr[idxStart : idxEnd+1]

	var keywords QueryKeywords
	if err := json.Unmarshal([]byte(jsonStr), &keywords); err != nil {
		return nil, fmt.Errorf("[extractQueryKeywords] failed to parse keywords: %w, response: %s", err, response)
	}

	return &keywords, nil
}

// findRelatedEntities finds entities related to a keyword through graph traversal.
func (r *Retriever) findRelatedEntities(ctx context.Context, keyword string, maxDepth int) ([]string, error) {
	if r.config.Graph == nil {
		return nil, fmt.Errorf("[findRelatedEntities] graph is not available")
	}

	entities := make(map[string]bool)
	visited := make(map[string]bool)

	// Start with the keyword itself as a potential entity
	queue := []struct {
		entity string
		depth  int
	}{{entity: keyword, depth: 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth > maxDepth {
			continue
		}

		if visited[current.entity] {
			continue
		}
		visited[current.entity] = true

		// Check if this entity exists in the graph (has any connections)
		neighbors, err := r.config.Graph.GetNeighbors(ctx, current.entity, "")
		if err == nil && len(neighbors) > 0 {
			entities[current.entity] = true
		}

		// Also check in-neighbors
		inNeighbors, err := r.config.Graph.GetInNeighbors(ctx, current.entity, "")
		if err == nil && len(inNeighbors) > 0 {
			entities[current.entity] = true
		}

		// If we found the entity, also explore its neighbors (but not through APPEARS_IN)
		if entities[current.entity] && current.depth < maxDepth {
			allNeighbors := append(neighbors, inNeighbors...)
			for _, neighbor := range allNeighbors {
				// Skip document links
				if !visited[neighbor] {
					queue = append(queue, struct {
						entity string
						depth  int
					}{entity: neighbor, depth: current.depth + 1})
				}
			}
		}
	}

	result := make([]string, 0, len(entities))
	for entity := range entities {
		result = append(result, entity)
	}

	return result, nil
}

// GetType returns the component type.
func (r *Retriever) GetType() string {
	return "Graph"
}

// IsCallbacksEnabled returns true as this component supports callbacks.
func (r *Retriever) IsCallbacksEnabled() bool {
	return true
}

// queryEntityExtractionPromptTemplate is the prompt template for extracting keywords from queries.
const queryEntityExtractionPromptTemplate = `
-Goal-
Extract both high-level and low-level keywords from the user's query. Always try to extract keywords for both categories when possible.

-Keyword Types-
High-level keywords: abstract themes, broad topics, conceptual categories, or domain areas that the query relates to (e.g., "Artificial Intelligence Technology", "Database Systems", "Web Development", "Machine Learning", "Software Engineering").
Low-level keywords: specific entities, proper nouns, people, organizations, acronyms, abbreviations, or precise technical terms mentioned directly in the query (e.g., "SQLiteAI", "GPT-4", "AIS", "John Doe", "OpenAI", "Python", "React").

-Classification Rules-
1. Acronyms and abbreviations (e.g., "AIS", "AI", "DB", "ML") should be classified as low-level keywords.
2. If an acronym represents a broader domain, also extract the conceptual theme as a high-level keyword (e.g., query "AIS" → low_level: ["AIS"], high_level: ["Artificial Intelligence Technology"]).
3. Specific technical terms, product names, and proper nouns should be low-level keywords.
4. Abstract concepts, themes, and domain categories should be high-level keywords.
5. A query can have both low-level and high-level keywords simultaneously.

-Examples-
Query: "What is AIS?"
  low_level: ["AIS"]
  high_level: ["Artificial Intelligence Technology"]

Query: "How does SQLiteAI work?"
  low_level: ["SQLiteAI"]
  high_level: ["Database Systems", "Database Technology"]

Query: "Tell me about machine learning algorithms"
  low_level: []
  high_level: ["Machine Learning", "Algorithms"]

-Output Format-
JSON object with two arrays (both arrays can contain multiple items or be empty):
{
  "low_level": ["Entity1", "Entity2", ...],
  "high_level": ["Theme1", "Theme2", ...]
}

-Query-
{query}
`

var queryEntityTemplate prompt.ChatTemplate

func init() {
	queryEntityTemplate = prompt.FromMessages(schema.FString,
		schema.UserMessage(queryEntityExtractionPromptTemplate),
	)
}

// getQueryEntityPrompt generates the query entity extraction prompt for the given query.
func getQueryEntityPrompt(ctx context.Context, query string) (string, error) {
	msgs, err := queryEntityTemplate.Format(ctx, map[string]any{"query": query})
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("no messages generated for query entity prompt")
	}
	return msgs[0].Content, nil
}
