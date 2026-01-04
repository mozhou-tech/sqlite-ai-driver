package graphsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Document 文档结构
type Document struct {
	ID       string
	Content  string
	MetaData map[string]any
}

// DocumentParser 文档解析器接口
type DocumentParser interface {
	Parse(ctx context.Context, reader io.Reader, opts ...interface{}) ([]*Document, error)
}

// IndexingOptions 索引选项
type IndexingOptions struct {
	// LLMGenerator 用于提取实体和关系的 LLM（可选，如果不提供则使用简单的启发式规则）
	LLMGenerator LLMGenerator
	// DocumentParser 自定义文档解析器（可选，用于支持 PDF/DOCX/HTML 等格式）
	DocumentParser DocumentParser
	// BatchSize 批量处理大小
	BatchSize int
	// MaxConcurrency 最大并发数
	MaxConcurrency int
	// DocumentMetadata 文档元数据
	DocumentMetadata map[string]any
}

// DefaultIndexingOptions 返回默认的索引选项
func DefaultIndexingOptions() *IndexingOptions {
	return &IndexingOptions{
		BatchSize:        10,
		MaxConcurrency:   5,
		DocumentMetadata: make(map[string]any),
	}
}

// IndexDocument 索引单个文档文件
// 支持的文件格式：PDF, DOCX, TXT, HTML
func (g *graphsearch) IndexDocument(ctx context.Context, filePath string, options *IndexingOptions) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	if options == nil {
		options = DefaultIndexingOptions()
	}

	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 根据文件扩展名选择解析器
	ext := strings.ToLower(filepath.Ext(filePath))
	var docs []*Document

	// 如果提供了自定义解析器，使用它
	if options != nil && options.DocumentParser != nil {
		parsedDocs, err := options.DocumentParser.Parse(ctx, file)
		if err != nil {
			return fmt.Errorf("failed to parse document with custom parser: %w", err)
		}
		docs = parsedDocs
	} else {
		// 使用内置的简单解析器（仅支持文本文件）
		switch ext {
		case ".txt":
			// 对于文本文件，直接读取内容
			content, err := io.ReadAll(file)
			if err != nil {
				return fmt.Errorf("failed to read text file: %w", err)
			}
			baseName := filepath.Base(filePath)
			docs = []*Document{
				{
					ID:      baseName,
					Content: string(content),
					MetaData: map[string]any{
						"source_file": filePath,
						"format":      "txt",
					},
				},
			}
		default:
			return fmt.Errorf("unsupported file format: %s (use custom DocumentParser for PDF/DOCX/HTML)", ext)
		}
	}

	if len(docs) == 0 {
		return fmt.Errorf("no documents extracted from file")
	}

	// 为文档添加元数据
	baseName := filepath.Base(filePath)
	for i, doc := range docs {
		if doc.ID == "" {
			if len(docs) == 1 {
				doc.ID = baseName
			} else {
				doc.ID = fmt.Sprintf("%s_part_%d", baseName, i+1)
			}
		}
		if doc.MetaData == nil {
			doc.MetaData = make(map[string]any)
		}
		doc.MetaData["source_file"] = filePath
		doc.MetaData["file_format"] = ext
		// 合并用户提供的元数据
		for k, v := range options.DocumentMetadata {
			doc.MetaData[k] = v
		}
	}

	// 索引文档
	return g.indexDocuments(ctx, docs, options)
}

// IndexDocuments 批量索引文档
func (g *graphsearch) IndexDocuments(ctx context.Context, filePaths []string, options *IndexingOptions) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	if options == nil {
		options = DefaultIndexingOptions()
	}

	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}

	// 批量处理文件
	for i := 0; i < len(filePaths); i += batchSize {
		end := i + batchSize
		if end > len(filePaths) {
			end = len(filePaths)
		}

		batch := filePaths[i:end]
		for _, filePath := range batch {
			if err := g.IndexDocument(ctx, filePath, options); err != nil {
				// 记录错误但继续处理其他文件
				_ = err
				continue
			}
		}
	}

	return nil
}

// indexDocuments 索引文档内容，提取实体和关系
func (g *graphsearch) indexDocuments(ctx context.Context, docs []*Document, options *IndexingOptions) error {
	for _, doc := range docs {
		if doc.Content == "" {
			continue
		}

		// 提取实体和关系
		var entities []ExtractedEntity
		var relationships []ExtractedRelationship

		if options.LLMGenerator != nil {
			// 使用 LLM 提取实体和关系
			extracted, err := g.extractWithLLM(ctx, doc.Content, options.LLMGenerator)
			if err != nil {
				// LLM 提取失败，回退到启发式方法
				// 回退到启发式方法
				entities, relationships = g.extractWithHeuristic(doc.Content)
			} else {
				entities = extracted.Entities
				relationships = extracted.Relationships
			}
		} else {
			// 使用启发式方法提取
			entities, relationships = g.extractWithHeuristic(doc.Content)
		}

		// 存储实体
		for _, entity := range entities {
			metadata := make(map[string]any)
			if entity.Type != "" {
				metadata["type"] = entity.Type
			}
			if entity.Description != "" {
				metadata["description"] = entity.Description
			}
			// 合并文档元数据
			if doc.MetaData != nil {
				for k, v := range doc.MetaData {
					metadata[k] = v
				}
			}

			if err := g.AddEntity(ctx, entity.Name, entity.Name, metadata); err != nil {
				// 记录错误但继续处理
				_ = err
				continue
			}

			// 链接实体到文档
			docID := doc.ID
			if docID == "" {
				docID = "unknown"
			}
			_ = g.Link(ctx, entity.Name, "APPEARS_IN", docID)

			// 存储实体类型
			if entity.Type != "" {
				_ = g.Link(ctx, entity.Name, "TYPE", entity.Type)
			}
		}

		// 存储关系
		for _, rel := range relationships {
			relation := rel.Relation
			if relation == "" {
				relation = "RELATED_TO"
			}

			_ = g.Link(ctx, rel.Source, relation, rel.Target)
		}

		// 文档已索引（可以在这里添加日志记录）
		_ = len(entities)
		_ = len(relationships)
	}

	return nil
}

// ExtractedEntity 提取的实体
type ExtractedEntity struct {
	Name        string
	Type        string
	Description string
}

// ExtractedRelationship 提取的关系
type ExtractedRelationship struct {
	Source      string
	Target      string
	Relation    string
	Description string
}

// ExtractionResult 提取结果
type ExtractionResult struct {
	Entities      []ExtractedEntity
	Relationships []ExtractedRelationship
}

// extractWithLLM 使用 LLM 提取实体和关系
func (g *graphsearch) extractWithLLM(ctx context.Context, text string, llm LLMGenerator) (*ExtractionResult, error) {
	// 使用改进的提示词
	prompt := g.buildImprovedExtractionPrompt(text)

	llmOptions := map[string]any{
		"temperature": 0.1,
		"max_tokens":  2000,
	}

	response, err := llm.Generate(ctx, prompt, llmOptions)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	// 解析 JSON 响应
	jsonStr := response
	idxStart := strings.Index(jsonStr, "{")
	idxEnd := strings.LastIndex(jsonStr, "}")
	if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
		// 尝试数组格式
		idxStart = strings.Index(jsonStr, "[")
		idxEnd = strings.LastIndex(jsonStr, "]")
		if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
			return nil, fmt.Errorf("no JSON object or array found in response")
		}
	}
	jsonStr = jsonStr[idxStart : idxEnd+1]

	var result struct {
		Entities      []ExtractedEntity       `json:"entities"`
		Relationships []ExtractedRelationship `json:"relationships"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse extraction result: %w", err)
	}

	return &ExtractionResult{
		Entities:      result.Entities,
		Relationships: result.Relationships,
	}, nil
}

// buildExtractionPrompt 构建提取提示词（保留向后兼容）
func (g *graphsearch) buildExtractionPrompt(text string) string {
	return g.buildImprovedExtractionPrompt(text)
}

// buildImprovedExtractionPrompt 构建改进的提取提示词
func (g *graphsearch) buildImprovedExtractionPrompt(text string) string {
	return fmt.Sprintf(`请从以下文本中提取实体和关系。

任务说明：
1. 仔细阅读文本内容
2. 识别所有有意义的实体（人物、地点、组织、概念等）
3. 识别实体之间的显式和隐式关系
4. 为每个实体指定合适的类型（Person, Organization, Location, Concept, Event, etc.）
5. 为每个关系提供清晰的描述

输出要求：
- 必须输出有效的 JSON 格式
- 实体名称要准确，与文本中一致
- 关系类型要具体明确（如：works_at, located_in, related_to, etc.）
- 描述要简洁但信息丰富

输出格式（JSON）：
{
  "entities": [
    {"name": "实体名称", "type": "类型", "description": "描述"}
  ],
  "relationships": [
    {"source": "源实体", "target": "目标实体", "relation": "关系", "description": "描述"}
  ]
}

文本：
%s

请直接输出 JSON 格式，不要包含其他文字或解释。`, text)
}

// extractWithHeuristic 使用启发式方法提取实体和关系（简单实现）
func (g *graphsearch) extractWithHeuristic(text string) ([]ExtractedEntity, []ExtractedRelationship) {
	var entities []ExtractedEntity
	var relationships []ExtractedRelationship

	// 简单的实体提取：查找首字母大写的词
	words := strings.Fields(text)
	entityMap := make(map[string]bool)

	for i, word := range words {
		// 移除标点符号
		cleanWord := strings.Trim(word, ".,!?;:()[]{}'\"")
		if len(cleanWord) > 1 && strings.ToUpper(cleanWord[:1]) == cleanWord[:1] {
			// 可能是实体
			if !entityMap[cleanWord] {
				entityMap[cleanWord] = true
				entities = append(entities, ExtractedEntity{
					Name:        cleanWord,
					Type:        "Unknown",
					Description: "",
				})
			}
		}

		// 简单的关系提取：查找常见的关系词
		if i < len(words)-1 {
			nextWord := strings.Trim(words[i+1], ".,!?;:()[]{}'\"")
			relation := ""
			switch cleanWord {
			case "的":
				relation = "has"
			case "属于":
				relation = "belongs_to"
			case "包含":
				relation = "contains"
			case "位于":
				relation = "located_in"
			case "与", "和":
				relation = "related_to"
			case "是":
				relation = "is_a"
			}

			if relation != "" && i > 0 {
				prevWord := strings.Trim(words[i-1], ".,!?;:()[]{}'\"")
				if len(prevWord) > 1 && len(nextWord) > 1 {
					relationships = append(relationships, ExtractedRelationship{
						Source:      prevWord,
						Target:      nextWord,
						Relation:    relation,
						Description: "",
					})
				}
			}
		}
	}

	return entities, relationships
}
