package graphsearch_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestPromptManager_LoadDefaultPrompts(t *testing.T) {
	manager := graphsearch.NewPromptManager("")
	manager.LoadDefaultPrompts()

	templates := manager.ListTemplates()
	if len(templates) == 0 {
		t.Error("期望有默认提示词模板")
	}

	// 检查关键模板是否存在
	expectedTemplates := []string{
		"entity_extraction",
		"query_processing",
		"community_summary",
		"answer_generation",
	}

	for _, expected := range expectedTemplates {
		found := false
		for _, template := range templates {
			if template == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("期望找到模板 %s，但未找到", expected)
		}
	}
}

func TestPromptManager_GetTemplate(t *testing.T) {
	manager := graphsearch.NewPromptManager("")
	manager.LoadDefaultPrompts()

	template, err := manager.GetTemplate("entity_extraction")
	if err != nil {
		t.Fatalf("GetTemplate() error = %v", err)
	}

	if template.Name != "entity_extraction" {
		t.Errorf("期望模板名称 = entity_extraction, 实际 = %s", template.Name)
	}

	if template.Template == "" {
		t.Error("期望模板内容非空")
	}
}

func TestPromptManager_FormatTemplate(t *testing.T) {
	manager := graphsearch.NewPromptManager("")
	manager.LoadDefaultPrompts()

	variables := map[string]any{
		"text": "测试文本",
	}

	formatted, err := manager.FormatTemplate(context.Background(), "entity_extraction", variables)
	if err != nil {
		t.Fatalf("FormatTemplate() error = %v", err)
	}

	if formatted == "" {
		t.Error("期望格式化后的提示词非空")
	}

	if !contains(formatted, "测试文本") {
		t.Error("期望格式化后的提示词包含变量值")
	}
}

func TestPromptManager_SaveAndLoadTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	manager := graphsearch.NewPromptManager(tmpDir)

	template := graphsearch.PromptTemplate{
		Name:        "test_template",
		Description: "测试模板",
		Template:    "这是一个测试模板，包含变量 {{variable}}",
		Variables:   []string{"variable"},
	}

	err := manager.SaveTemplate(template)
	if err != nil {
		t.Fatalf("SaveTemplate() error = %v", err)
	}

	// 验证文件是否存在
	filePath := filepath.Join(tmpDir, "test_template.yaml")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("期望模板文件已创建")
	}

	// 重新加载并验证
	loaded, err := manager.GetTemplate("test_template")
	if err != nil {
		t.Fatalf("GetTemplate() after save error = %v", err)
	}

	if loaded.Name != template.Name {
		t.Errorf("期望模板名称 = %s, 实际 = %s", template.Name, loaded.Name)
	}
}

func TestPromptManager_UpdateTemplate(t *testing.T) {
	manager := graphsearch.NewPromptManager("")
	manager.LoadDefaultPrompts()

	template, _ := manager.GetTemplate("entity_extraction")
	template.Description = "更新的描述"

	err := manager.UpdateTemplate("entity_extraction", template)
	if err != nil {
		t.Fatalf("UpdateTemplate() error = %v", err)
	}

	updated, err := manager.GetTemplate("entity_extraction")
	if err != nil {
		t.Fatalf("GetTemplate() after update error = %v", err)
	}

	if updated.Description != "更新的描述" {
		t.Errorf("期望描述 = 更新的描述, 实际 = %s", updated.Description)
	}
}

func TestPromptManager_DeleteTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	manager := graphsearch.NewPromptManager(tmpDir)

	template := graphsearch.PromptTemplate{
		Name:     "temp_template",
		Template: "临时模板",
	}

	manager.SaveTemplate(template)

	err := manager.DeleteTemplate("temp_template")
	if err != nil {
		t.Fatalf("DeleteTemplate() error = %v", err)
	}

	_, err = manager.GetTemplate("temp_template")
	if err == nil {
		t.Error("期望删除模板后 GetTemplate() 返回错误")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsHelper(s, substr))))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
