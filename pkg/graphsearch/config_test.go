package graphsearch_test

import (
	"os"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestDefaultConfig(t *testing.T) {
	config := graphsearch.DefaultConfig()
	if config == nil {
		t.Fatal("DefaultConfig() 返回 nil")
	}

	if config.GraphRAG.Retrieval.Limit == 0 {
		t.Error("期望默认检索 Limit > 0")
	}

	if config.LLM.Provider == "" {
		t.Error("期望默认 LLM Provider 非空")
	}
}

func TestConfigManager_LoadEnv(t *testing.T) {
	// 设置环境变量
	os.Setenv("OPENAI_API_KEY", "test-key")
	os.Setenv("OPENAI_BASE_URL", "https://test.api.com")
	os.Setenv("LLM_MODEL", "gpt-4-test")
	defer func() {
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("OPENAI_BASE_URL")
		os.Unsetenv("LLM_MODEL")
	}()

	manager := graphsearch.NewConfigManager("")
	err := manager.LoadEnv()
	if err != nil {
		t.Fatalf("LoadEnv() error = %v", err)
	}

	config := manager.GetConfig()
	if config.LLM.APIKey != "test-key" {
		t.Errorf("期望 APIKey = test-key, 实际 = %s", config.LLM.APIKey)
	}

	if config.LLM.BaseURL != "https://test.api.com" {
		t.Errorf("期望 BaseURL = https://test.api.com, 实际 = %s", config.LLM.BaseURL)
	}

	if config.LLM.Model != "gpt-4-test" {
		t.Errorf("期望 Model = gpt-4-test, 实际 = %s", config.LLM.Model)
	}
}

func TestConfigManager_GetGraphRAGOptions(t *testing.T) {
	manager := graphsearch.NewConfigManager("")
	manager.UpdateConfig(func(c *graphsearch.Config) {
		*c = *graphsearch.DefaultConfig()
	})

	options := manager.GetGraphRAGOptions()
	if options == nil {
		t.Fatal("GetGraphRAGOptions() 返回 nil")
	}

	if options.RetrievalOptions == nil {
		t.Error("期望 RetrievalOptions 非 nil")
	}

	if options.OrganizationOptions == nil {
		t.Error("期望 OrganizationOptions 非 nil")
	}

	if options.GenerationOptions == nil {
		t.Error("期望 GenerationOptions 非 nil")
	}
}

func TestGetEnv(t *testing.T) {
	os.Setenv("TEST_VAR", "test-value")
	defer os.Unsetenv("TEST_VAR")

	value := graphsearch.GetEnv("TEST_VAR", "default")
	if value != "test-value" {
		t.Errorf("期望 GetEnv() = test-value, 实际 = %s", value)
	}

	defaultValue := graphsearch.GetEnv("NON_EXISTENT_VAR", "default")
	if defaultValue != "default" {
		t.Errorf("期望 GetEnv() = default, 实际 = %s", defaultValue)
	}
}

func TestGetEnvBool(t *testing.T) {
	os.Setenv("TEST_BOOL_TRUE", "true")
	os.Setenv("TEST_BOOL_ONE", "1")
	os.Setenv("TEST_BOOL_FALSE", "false")
	defer func() {
		os.Unsetenv("TEST_BOOL_TRUE")
		os.Unsetenv("TEST_BOOL_ONE")
		os.Unsetenv("TEST_BOOL_FALSE")
	}()

	if !graphsearch.GetEnvBool("TEST_BOOL_TRUE", false) {
		t.Error("期望 GetEnvBool('TEST_BOOL_TRUE') = true")
	}

	if !graphsearch.GetEnvBool("TEST_BOOL_ONE", false) {
		t.Error("期望 GetEnvBool('TEST_BOOL_ONE') = true")
	}

	if graphsearch.GetEnvBool("TEST_BOOL_FALSE", true) {
		t.Error("期望 GetEnvBool('TEST_BOOL_FALSE') = false")
	}

	if !graphsearch.GetEnvBool("NON_EXISTENT", true) {
		t.Error("期望 GetEnvBool('NON_EXISTENT', true) = true")
	}
}

func TestGetEnvInt(t *testing.T) {
	os.Setenv("TEST_INT", "42")
	defer os.Unsetenv("TEST_INT")

	value := graphsearch.GetEnvInt("TEST_INT", 0)
	if value != 42 {
		t.Errorf("期望 GetEnvInt() = 42, 实际 = %d", value)
	}

	defaultValue := graphsearch.GetEnvInt("NON_EXISTENT", 100)
	if defaultValue != 100 {
		t.Errorf("期望 GetEnvInt() = 100, 实际 = %d", defaultValue)
	}
}
