package lightrag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SimpleLLM 简单的 LLM 实现，仅用于演示
type SimpleLLM struct {
}

func (l *SimpleLLM) Complete(ctx context.Context, prompt string) (string, error) {
	// 处理查询关键字提取 (LightRAG 论文定义)
	if strings.Contains(prompt, "high-level and low-level keywords") {
		lowLevel := `[]`
		highLevel := `[]`

		if strings.Contains(strings.ToLower(prompt), "sqliteai") {
			lowLevel = `["SQLiteAI"]`
		}
		if strings.Contains(strings.ToLower(prompt), "golang") {
			lowLevel = `["Golang"]`
		}
		if strings.Contains(strings.ToLower(prompt), "database") {
			highLevel = `["Database"]`
		}
		if strings.Contains(strings.ToLower(prompt), "javascript") {
			lowLevel = `["JavaScript"]`
		}
		if strings.Contains(strings.ToLower(prompt), "apple") {
			lowLevel = `["Apple"]`
		}
		if strings.Contains(strings.ToLower(prompt), "fruit") {
			highLevel = `["Fruit"]`
		}
		if strings.Contains(strings.ToLower(prompt), "ecosystem") {
			highLevel = `["Ecosystem"]`
		}
		if strings.Contains(strings.ToLower(prompt), "capital") {
			highLevel = `["Capital"]`
		}
		if strings.Contains(strings.ToLower(prompt), "database") && strings.Contains(strings.ToLower(prompt), "system") {
			highLevel = `["Database"]`
		}
		if strings.Contains(strings.ToLower(prompt), "mockentity") {
			lowLevel = `["MockEntity"]`
		}
		if strings.Contains(strings.ToLower(prompt), "fox") {
			lowLevel = `["Fox"]`
		}

		return fmt.Sprintf(`{
			"low_level": %s,
			"high_level": %s
		}`, lowLevel, highLevel), nil
	}

	// 处理旧的查询实体提取（保持兼容性，虽然可能不再使用）
	if strings.Contains(prompt, "Extract only the main entities") {
		if strings.Contains(prompt, "sqliteai") || strings.Contains(prompt, "SQLiteAI") {
			return `["SQLiteAI"]`, nil
		}
		return `["MockEntity"]`, nil
	}

	// 处理实体提取提示词
	if strings.Contains(prompt, "-Goal-") && strings.Contains(prompt, "entities") {
		if strings.Contains(prompt, "SQLiteAI") {
			return `{
				"entities": [{"name": "SQLiteAI", "type": "Database", "description": "SQLite AI driver"}],
				"relationships": [{"source": "SQLiteAI", "target": "Golang", "relation": "BUILT_FOR", "description": "Used in Go"}]
			}`, nil
		}
		if strings.Contains(prompt, "Apple") && strings.Contains(prompt, "fruit") {
			return `{
				"entities": [{"name": "Apple", "type": "Fruit", "description": "A type of fruit"}],
				"relationships": []
			}`, nil
		}
		if strings.Contains(prompt, "Apple") {
			return `{
				"entities": [{"name": "Apple", "type": "Company", "description": "Technology company"}],
				"relationships": []
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

// OpenAILLM OpenAI LLM 实现
type OpenAILLM struct {
	config *OpenAIConfig
	client *http.Client
}

// NewOpenAILLM 创建新的 OpenAI LLM 实例
func NewOpenAILLM(config *OpenAIConfig) *OpenAILLM {
	if config.Model == "" {
		config.Model = "gpt-4o-mini"
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	return &OpenAILLM{
		config: config,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Complete 完成提示词并返回响应
func (l *OpenAILLM) Complete(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("%s/chat/completions", l.config.BaseURL)

	reqBody := map[string]interface{}{
		"model": l.config.Model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.7,
		"extra_body": map[string]interface{}{
			"enable_thinking": false,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", l.config.APIKey))

	resp, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return result.Choices[0].Message.Content, nil
}
