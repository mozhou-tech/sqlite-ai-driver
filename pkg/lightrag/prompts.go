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
{{
  "low_level": ["Entity1", "Entity2", ...],
  "high_level": ["Theme1", "Theme2", ...]
}}

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
