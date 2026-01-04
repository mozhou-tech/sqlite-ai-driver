package graphsearch_test

import (
	"context"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestNewOpenAILLM(t *testing.T) {
	llm := graphsearch.NewOpenAILLM("test-key", "https://test.api.com", "gpt-4")
	if llm == nil {
		t.Fatal("NewOpenAILLM() 返回 nil")
	}

	if llm.APIKey != "test-key" {
		t.Errorf("期望 APIKey = test-key, 实际 = %s", llm.APIKey)
	}

	if llm.BaseURL != "https://test.api.com" {
		t.Errorf("期望 BaseURL = https://test.api.com, 实际 = %s", llm.BaseURL)
	}

	if llm.Model != "gpt-4" {
		t.Errorf("期望 Model = gpt-4, 实际 = %s", llm.Model)
	}
}

func TestNewOpenAILLM_Defaults(t *testing.T) {
	llm := graphsearch.NewOpenAILLM("test-key", "", "")
	if llm == nil {
		t.Fatal("NewOpenAILLM() 返回 nil")
	}

	if llm.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("期望默认 BaseURL = https://api.openai.com/v1, 实际 = %s", llm.BaseURL)
	}

	if llm.Model != "gpt-4" {
		t.Errorf("期望默认 Model = gpt-4, 实际 = %s", llm.Model)
	}
}

func TestNewAzureLLM(t *testing.T) {
	llm := graphsearch.NewAzureLLM("test-key", "https://test.endpoint.com", "deployment", "2024-02-15-preview")
	if llm == nil {
		t.Fatal("NewAzureLLM() 返回 nil")
	}

	if llm.APIKey != "test-key" {
		t.Errorf("期望 APIKey = test-key, 实际 = %s", llm.APIKey)
	}

	if llm.Endpoint != "https://test.endpoint.com" {
		t.Errorf("期望 Endpoint = https://test.endpoint.com, 实际 = %s", llm.Endpoint)
	}

	if llm.Deployment != "deployment" {
		t.Errorf("期望 Deployment = deployment, 实际 = %s", llm.Deployment)
	}
}

func TestLLMFactory_CreateLLM_OpenAI(t *testing.T) {
	factory := graphsearch.NewLLMFactory()

	config := map[string]string{
		"api_key":  "test-key",
		"base_url": "https://test.api.com",
		"model":    "gpt-4",
	}

	llm, err := factory.CreateLLM(graphsearch.ProviderOpenAI, config)
	if err != nil {
		t.Fatalf("CreateLLM() error = %v", err)
	}

	if llm == nil {
		t.Fatal("CreateLLM() 返回 nil")
	}
}

func TestLLMFactory_CreateLLM_Azure(t *testing.T) {
	factory := graphsearch.NewLLMFactory()

	config := map[string]string{
		"api_key":     "test-key",
		"endpoint":    "https://test.endpoint.com",
		"deployment":  "deployment",
		"api_version": "2024-02-15-preview",
	}

	llm, err := factory.CreateLLM(graphsearch.ProviderAzure, config)
	if err != nil {
		t.Fatalf("CreateLLM() error = %v", err)
	}

	if llm == nil {
		t.Fatal("CreateLLM() 返回 nil")
	}
}

func TestLLMFactory_CreateLLM_MissingConfig(t *testing.T) {
	factory := graphsearch.NewLLMFactory()

	// 缺少必需的配置
	config := map[string]string{}

	_, err := factory.CreateLLM(graphsearch.ProviderOpenAI, config)
	if err == nil {
		t.Error("期望 CreateLLM() 返回错误，但返回 nil")
	}
}

func TestLLMFactory_CreateLLMFromConfig(t *testing.T) {
	factory := graphsearch.NewLLMFactory()

	config := graphsearch.LLMConfig{
		Provider: "openai",
		APIKey:   "test-key",
		BaseURL:  "https://test.api.com",
		Model:    "gpt-4",
	}

	llm, err := factory.CreateLLMFromConfig(config)
	if err != nil {
		t.Fatalf("CreateLLMFromConfig() error = %v", err)
	}

	if llm == nil {
		t.Fatal("CreateLLMFromConfig() 返回 nil")
	}
}

func TestMockLLM_Generate(t *testing.T) {
	mockLLM := &MockLLMGenerator{}

	ctx := context.Background()
	response, err := mockLLM.Generate(ctx, "test prompt", nil)
	if err != nil {
		t.Fatalf("MockLLM.Generate() error = %v", err)
	}

	if response == "" {
		t.Error("期望 Generate() 返回非空响应")
	}
}
