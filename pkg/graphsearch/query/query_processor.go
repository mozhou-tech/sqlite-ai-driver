package graphsearch

import (
	"context"
	"fmt"
	"strings"
)

// defaultQueryProcessor 默认查询处理器实现
type defaultQueryProcessor struct {
	embedder    Embedder
	graphsearch *graphsearch
}

// NewDefaultQueryProcessor 创建默认查询处理器
func NewDefaultQueryProcessor(embedder Embedder, gs *graphsearch) QueryProcessor {
	return &defaultQueryProcessor{
		embedder:    embedder,
		graphsearch: gs,
	}
}

// ProcessQuery 处理原始查询，返回结构化查询
// 这是查询处理的核心方法，整合了实体识别、关系提取、查询分解、查询扩展和查询结构化
func (p *defaultQueryProcessor) ProcessQuery(ctx context.Context, query string) (*StructuredQuery, error) {
	// 验证输入
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	// 1. 提取实体
	entities, err := p.ExtractEntities(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to extract entities: %w", err)
	}

	// 2. 提取关系
	relations, err := p.ExtractRelations(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to extract relations: %w", err)
	}

	// 3. 分解查询
	subQueries, err := p.DecomposeQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to decompose query: %w", err)
	}

	// 4. 扩展查询
	expandedTerms, err := p.ExpandQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to expand query: %w", err)
	}

	// 5. 查询结构化：构建结构化查询对象
	structuredQuery := &StructuredQuery{
		OriginalQuery: query,
		Entities:      entities,
		Relations:     relations,
		SubQueries:    subQueries,
		ExpandedTerms: expandedTerms,
		Metadata:      make(map[string]any),
	}

	// 添加查询结构化元数据
	structuredQuery.Metadata["entity_count"] = len(entities)
	structuredQuery.Metadata["relation_count"] = len(relations)
	structuredQuery.Metadata["subquery_count"] = len(subQueries)
	structuredQuery.Metadata["expanded_term_count"] = len(expandedTerms)
	structuredQuery.Metadata["has_entities"] = len(entities) > 0
	structuredQuery.Metadata["has_relations"] = len(relations) > 0
	structuredQuery.Metadata["is_complex"] = len(subQueries) > 1

	return structuredQuery, nil
}

// ExtractEntities 从查询中提取命名实体
// 这是一个简化实现，实际应用中可以使用NER模型或规则引擎
func (p *defaultQueryProcessor) ExtractEntities(ctx context.Context, query string) ([]QueryEntity, error) {
	var entities []QueryEntity

	// 简单的基于关键词的实体提取
	// 实际应用中应该使用NER模型（如spaCy、Stanford NER等）
	words := strings.Fields(query)

	// 检查每个词是否可能是实体（这里使用简单的启发式规则）
	for _, word := range words {
		// 移除标点符号
		cleanWord := strings.Trim(word, ".,!?;:")

		// 如果词首字母大写且长度大于1，可能是实体
		if len(cleanWord) > 1 && strings.ToUpper(cleanWord[:1]) == cleanWord[:1] {
			// 检查该词是否在图谱中存在
			if p.graphsearch != nil && p.graphsearch.initialized {
				// 尝试在图中查找该实体
				entityInfo, err := p.graphsearch.GetEntity(ctx, cleanWord)
				if err == nil && entityInfo != nil {
					entities = append(entities, QueryEntity{
						Name:       cleanWord,
						Type:       "Unknown", // 可以从metadata中获取类型
						Confidence: 0.7,
						Metadata:   entityInfo,
					})
				} else {
					// 即使不在图中，也可能是新实体
					entities = append(entities, QueryEntity{
						Name:       cleanWord,
						Type:       "Unknown",
						Confidence: 0.5,
						Metadata:   make(map[string]any),
					})
				}
			} else {
				// 如果图未初始化，使用默认值
				entities = append(entities, QueryEntity{
					Name:       cleanWord,
					Type:       "Unknown",
					Confidence: 0.5,
					Metadata:   make(map[string]any),
				})
			}
		}
	}

	// 去重
	entityMap := make(map[string]bool)
	var uniqueEntities []QueryEntity
	for _, e := range entities {
		if !entityMap[e.Name] {
			entityMap[e.Name] = true
			uniqueEntities = append(uniqueEntities, e)
		}
	}

	return uniqueEntities, nil
}

// ExtractRelations 从查询中提取关系
// 这是一个简化实现，实际应用中可以使用关系抽取模型
func (p *defaultQueryProcessor) ExtractRelations(ctx context.Context, query string) ([]Relation, error) {
	var relations []Relation

	// 简单的基于关键词的关系提取
	// 常见的关系关键词
	relationKeywords := map[string]string{
		"的":  "has",
		"属于": "belongs_to",
		"包含": "contains",
		"位于": "located_in",
		"与":  "related_to",
		"和":  "related_to",
		"是":  "is_a",
		"有":  "has",
		"相关": "related_to",
	}

	words := strings.Fields(query)

	for i := 0; i < len(words)-1; i++ {
		word := strings.Trim(words[i], ".,!?;:")
		nextWord := strings.Trim(words[i+1], ".,!?;:")

		// 检查是否是关系关键词
		if predicate, ok := relationKeywords[word]; ok {
			// 尝试构建关系
			if i > 0 && i+2 < len(words) {
				subject := strings.Trim(words[i-1], ".,!?;:")
				object := nextWord

				relations = append(relations, Relation{
					Subject:    subject,
					Predicate:  predicate,
					Object:     object,
					Confidence: 0.6,
					Metadata:   make(map[string]any),
				})
			}
		}
	}

	return relations, nil
}

// DecomposeQuery 分解复杂查询为多个子查询
func (p *defaultQueryProcessor) DecomposeQuery(ctx context.Context, query string) ([]string, error) {
	var subQueries []string

	// 简单的基于分隔符的查询分解
	// 常见分隔符：和、或、以及、或者
	separators := []string{"和", "与", "以及", "或", "或者", ",", "，"}

	currentQuery := query
	for _, sep := range separators {
		parts := strings.Split(currentQuery, sep)
		if len(parts) > 1 {
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					subQueries = append(subQueries, trimmed)
				}
			}
			break
		}
	}

	// 如果没有找到分隔符，返回原始查询
	if len(subQueries) == 0 {
		subQueries = []string{query}
	}

	return subQueries, nil
}

// ExpandQuery 扩展查询（同义词、相关概念等）
func (p *defaultQueryProcessor) ExpandQuery(ctx context.Context, query string) ([]string, error) {
	var expandedTerms []string

	// 添加原始查询
	expandedTerms = append(expandedTerms, query)

	// 简单的同义词扩展（实际应用中应该使用同义词词典或词向量）
	// 这里只是示例
	synonyms := map[string][]string{
		"查找": {"搜索", "查询", "检索"},
		"信息": {"数据", "内容", "资料"},
		"关系": {"联系", "关联", "连接"},
	}

	words := strings.Fields(query)
	for _, word := range words {
		cleanWord := strings.Trim(word, ".,!?;:")
		if syns, ok := synonyms[cleanWord]; ok {
			expandedTerms = append(expandedTerms, syns...)
		}
	}

	// 去重
	termMap := make(map[string]bool)
	var uniqueTerms []string
	for _, term := range expandedTerms {
		if !termMap[term] {
			termMap[term] = true
			uniqueTerms = append(uniqueTerms, term)
		}
	}

	return uniqueTerms, nil
}
