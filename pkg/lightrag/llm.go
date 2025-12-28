package lightrag

import (
	"context"
	"fmt"
	"strings"
)

// SimpleLLM 简单的 LLM 实现，仅用于演示
type SimpleLLM struct {
}

func (l *SimpleLLM) Complete(ctx context.Context, prompt string) (string, error) {
	// 处理查询实体提取
	if strings.Contains(prompt, "Extract only the main entities") {
		if strings.Contains(prompt, "rxdb") || strings.Contains(prompt, "RxDB") {
			return `["RxDB"]`, nil
		}
		return `["MockEntity"]`, nil
	}

	// 处理实体提取提示词
	if strings.Contains(prompt, "-Goal-") && strings.Contains(prompt, "entities") {
		if strings.Contains(prompt, "RxDB") {
			return `{
				"entities": [{"name": "RxDB", "type": "Database", "description": "Reactive database"}],
				"relationships": [{"source": "RxDB", "target": "JavaScript", "relation": "BUILT_FOR", "description": "Used in JS"}]
			}`, nil
		}
		return `{
			"entities": [{"name": "MockEntity", "type": "MockType", "description": "MockDesc"}],
			"relationships": [{"source": "MockEntity", "target": "OtherEntity", "relation": "MOCK_REL", "description": "MockRelDesc"}]
		}`, nil
	}

	if strings.Contains(prompt, "Question:") {
		parts := strings.Split(prompt, "Question:")
		question := ""
		if len(parts) > 1 {
			question = strings.TrimSpace(strings.Split(parts[1], "\n")[0])
		}

		contextStr := ""
		if strings.Contains(prompt, "Context:") {
			cParts := strings.Split(prompt, "Context:")
			if len(cParts) > 1 {
				contextStr = strings.TrimSpace(strings.Split(cParts[1], "Question:")[0])
			}
		}

		return fmt.Sprintf("I am a simple LLM. Question: %s. Context: %s", question, contextStr), nil
	}
	return "Simple LLM response", nil
}

// OpenAIConfig OpenAI 配置
type OpenAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// TODO: 实现真正的 OpenAI LLM
