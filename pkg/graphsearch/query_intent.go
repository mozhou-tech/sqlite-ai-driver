package graphsearch

import (
	"context"
	"fmt"
	"strings"
)

// QueryIntent 查询意图
type QueryIntent string

const (
	IntentFactual       QueryIntent = "factual"       // 事实查询
	IntentRelational    QueryIntent = "relational"    // 关系查询
	IntentAnalytical    QueryIntent = "analytical"    // 分析查询
	IntentSummarization QueryIntent = "summarization" // 摘要查询
	IntentUnknown       QueryIntent = "unknown"       // 未知意图
)

// QueryIntentAnalyzer 查询意图分析器
type QueryIntentAnalyzer struct {
	llm LLMGenerator
}

// NewQueryIntentAnalyzer 创建查询意图分析器
func NewQueryIntentAnalyzer(llm LLMGenerator) *QueryIntentAnalyzer {
	return &QueryIntentAnalyzer{
		llm: llm,
	}
}

// AnalyzeIntent 分析查询意图
func (a *QueryIntentAnalyzer) AnalyzeIntent(ctx context.Context, query string) (QueryIntent, map[string]any, error) {
	if a.llm != nil {
		return a.analyzeWithLLM(ctx, query)
	}
	return a.analyzeWithHeuristic(query)
}

// analyzeWithLLM 使用 LLM 分析意图
func (a *QueryIntentAnalyzer) analyzeWithLLM(ctx context.Context, query string) (QueryIntent, map[string]any, error) {
	prompt := fmt.Sprintf(`请分析以下查询的意图。

查询：%s

请判断查询意图，可能的意图类型：
1. factual - 事实查询：询问具体的事实信息
2. relational - 关系查询：询问实体之间的关系
3. analytical - 分析查询：需要分析和推理
4. summarization - 摘要查询：需要生成摘要

请输出 JSON 格式：
{
  "intent": "意图类型",
  "confidence": 0.0-1.0,
  "keywords": ["关键词1", "关键词2"],
  "entities": ["实体1", "实体2"]
}`, query)

	llmOptions := map[string]any{
		"temperature": 0.1,
		"max_tokens":  200,
	}

	response, err := a.llm.Generate(ctx, prompt, llmOptions)
	if err != nil {
		// 回退到启发式方法
		return a.analyzeWithHeuristic(query)
	}

	// 解析 JSON 响应
	jsonStr := response
	idxStart := strings.Index(jsonStr, "{")
	idxEnd := strings.LastIndex(jsonStr, "}")
	if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
		return a.analyzeWithHeuristic(query)
	}
	jsonStr = jsonStr[idxStart : idxEnd+1]

	var result struct {
		Intent     string   `json:"intent"`
		Confidence float64  `json:"confidence"`
		Keywords   []string `json:"keywords"`
		Entities   []string `json:"entities"`
	}

	if err := parseJSON(jsonStr, &result); err != nil {
		return a.analyzeWithHeuristic(query)
	}

	metadata := map[string]any{
		"confidence": result.Confidence,
		"keywords":   result.Keywords,
		"entities":   result.Entities,
	}

	return QueryIntent(result.Intent), metadata, nil
}

// analyzeWithHeuristic 使用启发式方法分析意图
func (a *QueryIntentAnalyzer) analyzeWithHeuristic(query string) (QueryIntent, map[string]any, error) {
	queryLower := strings.ToLower(query)

	// 事实查询关键词
	factualKeywords := []string{"是什么", "什么是", "谁", "哪里", "何时", "多少", "如何", "what is", "who", "where", "when", "how many"}
	// 关系查询关键词
	relationalKeywords := []string{"关系", "联系", "相关", "关系", "relation", "related", "connect"}
	// 分析查询关键词
	analyticalKeywords := []string{"分析", "比较", "对比", "为什么", "原因", "analyze", "compare", "why", "reason"}
	// 摘要查询关键词
	summarizationKeywords := []string{"摘要", "总结", "概述", "summary", "summarize", "overview"}

	intent := IntentUnknown
	confidence := 0.5

	// 检查关键词
	for _, keyword := range factualKeywords {
		if strings.Contains(queryLower, keyword) {
			intent = IntentFactual
			confidence = 0.7
			break
		}
	}

	if intent == IntentUnknown {
		for _, keyword := range relationalKeywords {
			if strings.Contains(queryLower, keyword) {
				intent = IntentRelational
				confidence = 0.7
				break
			}
		}
	}

	if intent == IntentUnknown {
		for _, keyword := range analyticalKeywords {
			if strings.Contains(queryLower, keyword) {
				intent = IntentAnalytical
				confidence = 0.7
				break
			}
		}
	}

	if intent == IntentUnknown {
		for _, keyword := range summarizationKeywords {
			if strings.Contains(queryLower, keyword) {
				intent = IntentSummarization
				confidence = 0.7
				break
			}
		}
	}

	metadata := map[string]any{
		"confidence": confidence,
		"method":     "heuristic",
	}

	return intent, metadata, nil
}

// ConversationContext 对话上下文
type ConversationContext struct {
	History    []ConversationTurn
	MaxHistory int
}

// ConversationTurn 对话轮次
type ConversationTurn struct {
	Query    string
	Answer   string
	Intent   QueryIntent
	Entities []string
}

// NewConversationContext 创建对话上下文
func NewConversationContext(maxHistory int) *ConversationContext {
	if maxHistory <= 0 {
		maxHistory = 10
	}
	return &ConversationContext{
		History:    make([]ConversationTurn, 0),
		MaxHistory: maxHistory,
	}
}

// AddTurn 添加对话轮次
func (c *ConversationContext) AddTurn(query, answer string, intent QueryIntent, entities []string) {
	turn := ConversationTurn{
		Query:    query,
		Answer:   answer,
		Intent:   intent,
		Entities: entities,
	}

	c.History = append(c.History, turn)

	// 限制历史长度
	if len(c.History) > c.MaxHistory {
		c.History = c.History[1:]
	}
}

// GetRelevantEntities 获取相关实体（从历史中提取）
func (c *ConversationContext) GetRelevantEntities() []string {
	entitySet := make(map[string]bool)
	for _, turn := range c.History {
		for _, entity := range turn.Entities {
			entitySet[entity] = true
		}
	}

	entities := make([]string, 0, len(entitySet))
	for entity := range entitySet {
		entities = append(entities, entity)
	}
	return entities
}

// GetContextSummary 获取上下文摘要
func (c *ConversationContext) GetContextSummary() string {
	if len(c.History) == 0 {
		return ""
	}

	var summary strings.Builder
	summary.WriteString("对话历史：\n")
	for i, turn := range c.History {
		summary.WriteString(fmt.Sprintf("%d. 问题：%s\n", i+1, turn.Query))
		if turn.Answer != "" {
			summary.WriteString(fmt.Sprintf("   回答：%s\n", truncateString(turn.Answer, 100)))
		}
	}
	return summary.String()
}

// EnhancedQueryProcessor 增强的查询处理器（支持意图识别和多轮对话）
type EnhancedQueryProcessor struct {
	*defaultQueryProcessor
	intentAnalyzer      *QueryIntentAnalyzer
	conversationContext *ConversationContext
}

// NewEnhancedQueryProcessor 创建增强的查询处理器
func NewEnhancedQueryProcessor(embedder Embedder, gs *graphsearch, llm LLMGenerator, maxHistory int) *EnhancedQueryProcessor {
	return &EnhancedQueryProcessor{
		defaultQueryProcessor: NewDefaultQueryProcessor(embedder, gs).(*defaultQueryProcessor),
		intentAnalyzer:        NewQueryIntentAnalyzer(llm),
		conversationContext:   NewConversationContext(maxHistory),
	}
}

// ProcessQueryWithContext 处理查询（带上下文）
func (p *EnhancedQueryProcessor) ProcessQueryWithContext(ctx context.Context, query string) (*StructuredQuery, QueryIntent, error) {
	// 分析查询意图
	intent, intentMetadata, err := p.intentAnalyzer.AnalyzeIntent(ctx, query)
	if err != nil {
		intent = IntentUnknown
	}

	// 从上下文中获取相关实体
	contextEntities := p.conversationContext.GetRelevantEntities()

	// 处理查询
	structuredQuery, err := p.ProcessQuery(ctx, query)
	if err != nil {
		return nil, intent, fmt.Errorf("failed to process query: %w", err)
	}

	// 合并上下文实体
	if len(contextEntities) > 0 {
		entitySet := make(map[string]bool)
		for _, e := range structuredQuery.Entities {
			entitySet[e.Name] = true
		}
		for _, e := range contextEntities {
			if !entitySet[e] {
				structuredQuery.Entities = append(structuredQuery.Entities, QueryEntity{
					Name:       e,
					Type:       "Unknown",
					Confidence: 0.5,
					Metadata:   map[string]any{"from_context": true},
				})
			}
		}
	}

	// 添加意图信息到元数据
	structuredQuery.Metadata["intent"] = string(intent)
	for k, v := range intentMetadata {
		structuredQuery.Metadata[k] = v
	}

	return structuredQuery, intent, nil
}

// UpdateContext 更新对话上下文
func (p *EnhancedQueryProcessor) UpdateContext(query, answer string, intent QueryIntent, entities []string) {
	p.conversationContext.AddTurn(query, answer, intent, entities)
}

// GetContext 获取对话上下文
func (p *EnhancedQueryProcessor) GetContext() *ConversationContext {
	return p.conversationContext
}

// ClearContext 清空对话上下文
func (p *EnhancedQueryProcessor) ClearContext() {
	p.conversationContext = NewConversationContext(p.conversationContext.MaxHistory)
}

// Helper functions

func parseJSON(jsonStr string, v interface{}) error {
	// 简单的 JSON 解析（实际应该使用 encoding/json）
	// 这里简化处理
	return nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
