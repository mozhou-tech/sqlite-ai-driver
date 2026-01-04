package graphsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LLMProvider LLM 提供商类型
type LLMProvider string

const (
	ProviderOpenAI LLMProvider = "openai"
	ProviderAzure  LLMProvider = "azure"
	ProviderCustom LLMProvider = "custom"
)

// OpenAILLM OpenAI LLM 实现
type OpenAILLM struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
}

// NewOpenAILLM 创建 OpenAI LLM 实例
func NewOpenAILLM(apiKey, baseURL, model string) *OpenAILLM {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4"
	}

	return &OpenAILLM{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
		Client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Generate 生成文本
func (llm *OpenAILLM) Generate(ctx context.Context, prompt string, options map[string]any) (string, error) {
	// 构建请求
	url := fmt.Sprintf("%s/chat/completions", llm.BaseURL)

	temperature := 0.7
	if temp, ok := options["temperature"].(float64); ok {
		temperature = temp
	}

	maxTokens := 1000
	if max, ok := options["max_tokens"].(int); ok {
		maxTokens = max
	} else if max, ok := options["max_length"].(int); ok {
		maxTokens = max
	}

	requestBody := map[string]any{
		"model": llm.Model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": temperature,
		"max_tokens":  maxTokens,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", llm.APIKey))

	resp, err := llm.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return response.Choices[0].Message.Content, nil
}

// AzureLLM Azure OpenAI LLM 实现
type AzureLLM struct {
	APIKey     string
	Endpoint   string
	Deployment string
	APIVersion string
	Client     *http.Client
}

// NewAzureLLM 创建 Azure LLM 实例
func NewAzureLLM(apiKey, endpoint, deployment, apiVersion string) *AzureLLM {
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}

	return &AzureLLM{
		APIKey:     apiKey,
		Endpoint:   endpoint,
		Deployment: deployment,
		APIVersion: apiVersion,
		Client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Generate 生成文本
func (llm *AzureLLM) Generate(ctx context.Context, prompt string, options map[string]any) (string, error) {
	// 构建请求 URL
	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		llm.Endpoint, llm.Deployment, llm.APIVersion)

	temperature := 0.7
	if temp, ok := options["temperature"].(float64); ok {
		temperature = temp
	}

	maxTokens := 1000
	if max, ok := options["max_tokens"].(int); ok {
		maxTokens = max
	} else if max, ok := options["max_length"].(int); ok {
		maxTokens = max
	}

	requestBody := map[string]any{
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": temperature,
		"max_tokens":  maxTokens,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", llm.APIKey)

	resp, err := llm.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return response.Choices[0].Message.Content, nil
}

// LLMFactory LLM 工厂
type LLMFactory struct{}

// NewLLMFactory 创建 LLM 工厂
func NewLLMFactory() *LLMFactory {
	return &LLMFactory{}
}

// CreateLLM 根据配置创建 LLM 实例
func (f *LLMFactory) CreateLLM(provider LLMProvider, config map[string]string) (LLMGenerator, error) {
	switch provider {
	case ProviderOpenAI:
		apiKey := config["api_key"]
		baseURL := config["base_url"]
		model := config["model"]
		if apiKey == "" {
			return nil, fmt.Errorf("API key is required for OpenAI")
		}
		return NewOpenAILLM(apiKey, baseURL, model), nil

	case ProviderAzure:
		apiKey := config["api_key"]
		endpoint := config["endpoint"]
		deployment := config["deployment"]
		apiVersion := config["api_version"]
		if apiKey == "" || endpoint == "" || deployment == "" {
			return nil, fmt.Errorf("API key, endpoint, and deployment are required for Azure")
		}
		return NewAzureLLM(apiKey, endpoint, deployment, apiVersion), nil

	case ProviderCustom:
		// 自定义 LLM 需要用户提供实现
		return nil, fmt.Errorf("custom LLM provider requires custom implementation")

	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", provider)
	}
}

// CreateLLMFromConfig 从配置创建 LLM 实例
func (f *LLMFactory) CreateLLMFromConfig(config LLMConfig) (LLMGenerator, error) {
	providerConfig := make(map[string]string)
	providerConfig["api_key"] = config.APIKey
	providerConfig["base_url"] = config.BaseURL
	providerConfig["model"] = config.Model

	// 从 Options 中提取额外配置
	if config.Options != nil {
		if endpoint, ok := config.Options["endpoint"].(string); ok {
			providerConfig["endpoint"] = endpoint
		}
		if deployment, ok := config.Options["deployment"].(string); ok {
			providerConfig["deployment"] = deployment
		}
		if apiVersion, ok := config.Options["api_version"].(string); ok {
			providerConfig["api_version"] = apiVersion
		}
	}

	provider := LLMProvider(config.Provider)
	if provider == "" {
		provider = ProviderOpenAI
	}

	return f.CreateLLM(provider, providerConfig)
}
