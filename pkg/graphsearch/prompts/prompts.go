package prompts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PromptTemplate 提示词模板
type PromptTemplate struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Template    string         `yaml:"template"`
	Variables   []string       `yaml:"variables"`
	Metadata    map[string]any `yaml:"metadata"`
}

// PromptConfig 提示词配置
type PromptConfig struct {
	Templates map[string]PromptTemplate `yaml:"templates"`
	Defaults  map[string]string         `yaml:"defaults"`
}

// PromptManager 提示词管理器
type PromptManager struct {
	config    *PromptConfig
	templates map[string]PromptTemplate
	customDir string
}

// NewPromptManager 创建提示词管理器
func NewPromptManager(customDir string) *PromptManager {
	return &PromptManager{
		config:    &PromptConfig{Templates: make(map[string]PromptTemplate), Defaults: make(map[string]string)},
		templates: make(map[string]PromptTemplate),
		customDir: customDir,
	}
}

// LoadConfig 加载提示词配置
func (pm *PromptManager) LoadConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config PromptConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	pm.config = &config
	pm.templates = config.Templates

	return nil
}

// LoadDefaultPrompts 加载默认提示词
func (pm *PromptManager) LoadDefaultPrompts() {
	// 实体提取提示词
	pm.templates["entity_extraction"] = PromptTemplate{
		Name:        "entity_extraction",
		Description: "实体和关系提取提示词",
		Template: `请从以下文本中提取实体和关系。

目标：
1. 识别文本中的所有实体。对于每个实体，指定其名称、类型和简要描述。
2. 识别实体之间的所有关系。对于每个关系，指定源实体、目标实体、关系名称和简要描述。

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
{{text}}

请直接输出 JSON 格式，不要包含其他文字。`,
		Variables: []string{"text"},
	}

	// 查询处理提示词
	pm.templates["query_processing"] = PromptTemplate{
		Name:        "query_processing",
		Description: "查询处理提示词",
		Template: `请分析以下查询，提取实体、关系和查询意图。

查询：{{query}}

请输出 JSON 格式：
{
  "entities": ["实体1", "实体2"],
  "relations": [{"source": "源", "target": "目标", "relation": "关系"}],
  "intent": "查询意图"
}`,
		Variables: []string{"query"},
	}

	// 社区摘要提示词
	pm.templates["community_summary"] = PromptTemplate{
		Name:        "community_summary",
		Description: "社区摘要提示词",
		Template: `请为以下实体社区生成摘要。

根实体：{{root_entity}}

社区中的实体（共 {{entity_count}} 个）：
{{entities}}

社区中的关系（共 {{relation_count}} 个）：
{{relations}}

请生成一个社区摘要，描述该社区的整体特征、主要实体和它们之间的关系。`,
		Variables: []string{"root_entity", "entity_count", "entities", "relation_count", "relations"},
	}

	// 答案生成提示词
	pm.templates["answer_generation"] = PromptTemplate{
		Name:        "answer_generation",
		Description: "答案生成提示词",
		Template: `基于以下图谱信息回答问题。

问题：{{query}}

图谱信息：
{{graph_context}}

请基于以上信息回答问题。`,
		Variables: []string{"query", "graph_context"},
	}
}

// GetTemplate 获取提示词模板
func (pm *PromptManager) GetTemplate(name string) (PromptTemplate, error) {
	template, ok := pm.templates[name]
	if !ok {
		return PromptTemplate{}, fmt.Errorf("template not found: %s", name)
	}
	return template, nil
}

// FormatTemplate 格式化提示词模板
func (pm *PromptManager) FormatTemplate(ctx context.Context, name string, variables map[string]any) (string, error) {
	template, err := pm.GetTemplate(name)
	if err != nil {
		return "", err
	}

	result := template.Template

	// 替换变量
	for key, value := range variables {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", value))
	}

	// 检查是否还有未替换的变量
	for _, varName := range template.Variables {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		if strings.Contains(result, placeholder) {
			// 如果有默认值，使用默认值
			if defaultValue, ok := pm.config.Defaults[varName]; ok {
				result = strings.ReplaceAll(result, placeholder, defaultValue)
			} else {
				return "", fmt.Errorf("variable %s not provided and no default value", varName)
			}
		}
	}

	return result, nil
}

// SaveTemplate 保存提示词模板到文件
func (pm *PromptManager) SaveTemplate(template PromptTemplate) error {
	if pm.customDir == "" {
		return fmt.Errorf("custom directory not set")
	}

	// 确保目录存在
	if err := os.MkdirAll(pm.customDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 保存模板
	filePath := filepath.Join(pm.customDir, fmt.Sprintf("%s.yaml", template.Name))
	data, err := yaml.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	// 更新内存中的模板
	pm.templates[template.Name] = template

	return nil
}

// ListTemplates 列出所有模板
func (pm *PromptManager) ListTemplates() []string {
	names := make([]string, 0, len(pm.templates))
	for name := range pm.templates {
		names = append(names, name)
	}
	return names
}

// UpdateTemplate 更新模板
func (pm *PromptManager) UpdateTemplate(name string, template PromptTemplate) error {
	if _, ok := pm.templates[name]; !ok {
		return fmt.Errorf("template not found: %s", name)
	}

	pm.templates[name] = template

	// 如果设置了自定义目录，保存到文件
	if pm.customDir != "" {
		return pm.SaveTemplate(template)
	}

	// 如果没有设置自定义目录，只更新内存中的模板
	return nil
}

// DeleteTemplate 删除模板
func (pm *PromptManager) DeleteTemplate(name string) error {
	if _, ok := pm.templates[name]; !ok {
		return fmt.Errorf("template not found: %s", name)
	}

	delete(pm.templates, name)

	if pm.customDir != "" {
		filePath := filepath.Join(pm.customDir, fmt.Sprintf("%s.yaml", name))
		_ = os.Remove(filePath)
	}

	return nil
}

// SaveConfig 保存配置到文件
func (pm *PromptManager) SaveConfig(configPath string) error {
	pm.config.Templates = pm.templates

	data, err := yaml.Marshal(pm.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
