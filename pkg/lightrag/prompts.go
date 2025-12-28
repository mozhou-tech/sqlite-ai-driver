package lightrag

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
)

const (
	EntityExtractionPromptTemplate = `
-Goal-
Identify entities and relationships from the given text.

-Steps-
1. Identify all entities in the text. For each entity, specify its name, type, and a brief description.
2. Identify all relationships between the entities. For each relationship, specify the source entity, target entity, relationship name, and a brief description.
3. Output the results in JSON format as follows:
{{
  "entities": [{{ "name": "Entity Name", "type": "Type", "description": "Description" }}],
  "relationships": [{{ "source": "Source", "target": "Target", "relation": "Relation", "description": "Description" }}]
}}

-Text-
{text}
`

	QueryEntityExtractionPromptTemplate = `
-Goal-
Extract only the main entities mentioned in the following query.

-Output Format-
JSON array of entity names: ["Entity1", "Entity2", ...]

-Query-
{query}
`

	RAGAnswerPromptTemplate = `
Context:
{context}

Question: {query}

Answer the question based on the context.
`
)

var (
	entityExtractionTemplate prompt.ChatTemplate
	queryEntityTemplate      prompt.ChatTemplate
	ragAnswerTemplate        prompt.ChatTemplate
)

func init() {
	entityExtractionTemplate = prompt.FromMessages(schema.FString,
		schema.UserMessage(EntityExtractionPromptTemplate),
	)

	queryEntityTemplate = prompt.FromMessages(schema.FString,
		schema.UserMessage(QueryEntityExtractionPromptTemplate),
	)

	ragAnswerTemplate = prompt.FromMessages(schema.FString,
		schema.UserMessage(RAGAnswerPromptTemplate),
	)
}

type ExtractionResult struct {
	Entities      []Entity       `json:"entities"`
	Relationships []Relationship `json:"relationships"`
}

func GetExtractionPrompt(ctx context.Context, text string) (string, error) {
	msgs, err := entityExtractionTemplate.Format(ctx, map[string]any{"text": text})
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("no messages generated for extraction prompt")
	}
	return msgs[0].Content, nil
}

func GetQueryEntityPrompt(ctx context.Context, query string) (string, error) {
	msgs, err := queryEntityTemplate.Format(ctx, map[string]any{"query": query})
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("no messages generated for query entity prompt")
	}
	return msgs[0].Content, nil
}

func GetRAGAnswerPrompt(ctx context.Context, contextText, query string) (string, error) {
	msgs, err := ragAnswerTemplate.Format(ctx, map[string]any{
		"context": contextText,
		"query":   query,
	})
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("no messages generated for RAG answer prompt")
	}
	return msgs[0].Content, nil
}
