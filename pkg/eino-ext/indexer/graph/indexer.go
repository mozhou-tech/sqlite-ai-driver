package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphstore"
	"github.com/sirupsen/logrus"
)

// LLM defines the interface for language models used to extract entities and relationships.
type LLM interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// Entity represents an entity extracted from a document.
type Entity struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Relationship represents a relationship between entities.
type Relationship struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	Relation    string `json:"relation"`
	Description string `json:"description"`
}

// ExtractionResult contains the extracted entities and relationships from a document.
type ExtractionResult struct {
	Entities      []Entity       `json:"entities"`
	Relationships []Relationship `json:"relationships"`
}

// IndexerConfig defines the configuration for the Graph indexer.
type IndexerConfig struct {
	// Graph is the GraphStore instance to use for indexing.
	Graph *graphstore.GraphStore
	// LLM is the language model to use for extracting entities and relationships.
	LLM LLM
	// DocumentToMap optionally overrides the default conversion from eino document to map.
	DocumentToMap func(ctx context.Context, doc *schema.Document) (map[string]any, error)
	// Transformer optionally transforms documents before indexing (e.g. splitting).
	Transformer document.Transformer
}

// Indexer implements the Eino indexer.Indexer interface for Graph-based indexing.
type Indexer struct {
	config *IndexerConfig
}

// NewIndexer creates a new Graph indexer.
func NewIndexer(ctx context.Context, config *IndexerConfig) (*Indexer, error) {
	if config.Graph == nil {
		return nil, fmt.Errorf("[NewIndexer] graph instance not provided")
	}

	if config.LLM == nil {
		return nil, fmt.Errorf("[NewIndexer] LLM instance not provided")
	}

	if config.DocumentToMap == nil {
		config.DocumentToMap = defaultDocumentToMap
	}

	// Ensure GraphStore is initialized
	if err := config.Graph.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("[NewIndexer] failed to initialize graph store: %w", err)
	}

	return &Indexer{
		config: config,
	}, nil
}

// Store indexes the provided documents into the graph database.
func (i *Indexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) (ids []string, err error) {
	if i == nil {
		return nil, fmt.Errorf("[Store] Indexer instance is nil")
	}
	if i.config == nil {
		return nil, fmt.Errorf("[Store] config is nil")
	}

	ctx = callbacks.EnsureRunInfo(ctx, i.GetType(), components.ComponentOfIndexer)
	ctx = callbacks.OnStart(ctx, &indexer.CallbackInput{Docs: docs})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	// Transform documents if transformer is provided
	if i.config.Transformer != nil {
		docs, err = i.config.Transformer.Transform(ctx, docs)
		if err != nil {
			return nil, fmt.Errorf("[Store] failed to transform documents: %w", err)
		}
	}
	if docs == nil {
		return nil, fmt.Errorf("[Store] transformed documents is nil")
	}

	ids = make([]string, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			return nil, fmt.Errorf("[Store] document is nil")
		}

		docID := doc.ID
		if docID == "" {
			return nil, fmt.Errorf("[Store] document ID is empty")
		}

		// Convert document to map
		docMap, err := i.config.DocumentToMap(ctx, doc)
		if err != nil {
			return nil, fmt.Errorf("[Store] failed to convert document to map: %w", err)
		}
		if docMap == nil {
			return nil, fmt.Errorf("[Store] DocumentToMap returned nil map")
		}

		// Extract content from document map
		content, ok := docMap["content"].(string)
		if !ok {
			// Try to get content from document directly
			content = doc.Content
		}

		if content == "" {
			logrus.WithField("doc_id", docID).Warn("Document content is empty, skipping graph extraction")
			ids = append(ids, docID)
			continue
		}

		// Extract entities and relationships from content using LLM
		err = i.extractAndStore(ctx, content, docID)
		if err != nil {
			logrus.WithError(err).WithField("doc_id", docID).Error("Failed to extract and store graph data")
			// Continue processing other documents even if one fails
			ids = append(ids, docID)
			continue
		}

		ids = append(ids, docID)
	}

	callbacks.OnEnd(ctx, &indexer.CallbackOutput{IDs: ids})

	return ids, nil
}

// extractAndStore extracts entities and relationships from text and stores them in the graph.
func (i *Indexer) extractAndStore(ctx context.Context, text string, docID string) error {
	if i.config.LLM == nil {
		return fmt.Errorf("[extractAndStore] LLM is not available")
	}
	if i.config.Graph == nil {
		return fmt.Errorf("[extractAndStore] graph database is not available")
	}

	// Get extraction prompt
	promptStr, err := getExtractionPrompt(ctx, text)
	if err != nil {
		return fmt.Errorf("[extractAndStore] failed to get extraction prompt: %w", err)
	}

	// Call LLM to extract entities and relationships
	response, err := i.config.LLM.Complete(ctx, promptStr)
	if err != nil {
		return fmt.Errorf("[extractAndStore] failed to complete LLM request: %w", err)
	}

	// Parse JSON response
	jsonStr := response
	idxStart := strings.Index(jsonStr, "{")
	idxEnd := strings.LastIndex(jsonStr, "}")
	if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
		// Try array format
		idxStart = strings.Index(jsonStr, "[")
		idxEnd = strings.LastIndex(jsonStr, "]")
		if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
			return fmt.Errorf("[extractAndStore] no JSON object or array found in response: %s", response)
		}
	}
	jsonStr = jsonStr[idxStart : idxEnd+1]

	var result ExtractionResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return fmt.Errorf("[extractAndStore] failed to parse extraction result: %w, response: %s", err, response)
	}

	logrus.WithFields(logrus.Fields{
		"doc_id":              docID,
		"entities_count":      len(result.Entities),
		"relationships_count": len(result.Relationships),
	}).Info("Extracted graph data from document")

	// Store entities and link them to the document
	for _, entity := range result.Entities {
		if entity.Name == "" {
			continue
		}

		// Link entity to document
		err := i.config.Graph.Link(ctx, entity.Name, "APPEARS_IN", docID)
		if err != nil {
			logrus.WithError(err).Errorf("Failed to link entity %s to doc %s", entity.Name, docID)
		}

		// Store entity type
		if entity.Type != "" {
			_ = i.config.Graph.Link(ctx, entity.Name, "TYPE", entity.Type)
		}

		// Store entity description
		if entity.Description != "" {
			_ = i.config.Graph.Link(ctx, entity.Name, "DESCRIPTION", entity.Description)
		}
	}

	// Store relationships
	for _, rel := range result.Relationships {
		if rel.Source == "" || rel.Target == "" {
			continue
		}

		relation := rel.Relation
		if relation == "" {
			relation = "RELATED_TO"
		}

		err := i.config.Graph.Link(ctx, rel.Source, relation, rel.Target)
		if err != nil {
			logrus.WithError(err).Errorf("Failed to link nodes: %s -[%s]-> %s", rel.Source, relation, rel.Target)
		}
	}

	return nil
}

// GetType returns the component type.
func (i *Indexer) GetType() string {
	return "Graph"
}

// IsCallbacksEnabled returns true as this component supports callbacks.
func (i *Indexer) IsCallbacksEnabled() bool {
	return true
}

// defaultDocumentToMap converts an eino document to a map.
func defaultDocumentToMap(ctx context.Context, doc *schema.Document) (map[string]any, error) {
	if doc == nil {
		return nil, fmt.Errorf("[defaultDocumentToMap] document is nil")
	}

	metaDataLen := 0
	if doc.MetaData != nil {
		metaDataLen = len(doc.MetaData)
	}

	docMap := make(map[string]any, metaDataLen+2)
	docMap["id"] = doc.ID
	docMap["content"] = doc.Content

	if doc.MetaData != nil {
		for k, v := range doc.MetaData {
			docMap[k] = v
		}
	}

	return docMap, nil
}

// entityExtractionPromptTemplate is the prompt template for extracting entities and relationships.
const entityExtractionPromptTemplate = `
-Goal-
Identify entities and relationships from the given text.

-Steps-
1. Identify all entities in the text. For each entity, specify its name, type, and a brief description.
2. Identify all relationships between the entities. For each relationship, specify the source entity, target entity, relationship name, and a brief description.
3. Output the results in JSON format as follows:
{
  "entities": [{"name": "Entity Name", "type": "Type", "description": "Description"}],
  "relationships": [{"source": "Source", "target": "Target", "relation": "Relation", "description": "Description"}]
}

-Text-
{text}
`

var entityExtractionTemplate prompt.ChatTemplate

func init() {
	entityExtractionTemplate = prompt.FromMessages(schema.FString,
		schema.UserMessage(entityExtractionPromptTemplate),
	)
}

// getExtractionPrompt generates the extraction prompt for the given text.
func getExtractionPrompt(ctx context.Context, text string) (string, error) {
	msgs, err := entityExtractionTemplate.Format(ctx, map[string]any{"text": text})
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("no messages generated for extraction prompt")
	}
	return msgs[0].Content, nil
}
