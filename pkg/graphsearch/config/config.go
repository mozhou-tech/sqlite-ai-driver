package graphsearch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config 配置结构
type Config struct {
	// GraphRAG 配置
	GraphRAG GraphRAGConfig `yaml:"graphrag"`
	// LLM 配置
	LLM LLMConfig `yaml:"llm"`
	// Embedding 配置
	Embedding EmbeddingConfig `yaml:"embedding"`
	// Storage 配置
	Storage StorageConfig `yaml:"storage"`
	// Prompt 配置
	Prompt PromptStorageConfig `yaml:"prompt"`
}

// GraphRAGConfig GraphRAG 配置
type GraphRAGConfig struct {
	// Retrieval 检索配置
	Retrieval RetrievalConfig `yaml:"retrieval"`
	// Organization 组织配置
	Organization OrganizationConfig `yaml:"organization"`
	// Generation 生成配置
	Generation GenerationConfig `yaml:"generation"`
}

// RetrievalConfig 检索配置
type RetrievalConfig struct {
	Limit               int     `yaml:"limit"`
	MaxDepth            int     `yaml:"max_depth"`
	SimilarityThreshold float64 `yaml:"similarity_threshold"`
	Strategy            string  `yaml:"strategy"`
}

// OrganizationConfig 组织配置
type OrganizationConfig struct {
	EnablePruning       bool            `yaml:"enable_pruning"`
	EnableReranking     bool            `yaml:"enable_reranking"`
	EnableAugmentation  bool            `yaml:"enable_augmentation"`
	EnableVerbalization bool            `yaml:"enable_verbalization"`
	Pruning             PruningConfig   `yaml:"pruning"`
	Reranking           RerankingConfig `yaml:"reranking"`
}

// PruningConfig 剪枝配置
type PruningConfig struct {
	MaxNodes         int     `yaml:"max_nodes"`
	MaxEdges         int     `yaml:"max_edges"`
	MinScore         float64 `yaml:"min_score"`
	KeepCoreEntities bool    `yaml:"keep_core_entities"`
}

// RerankingConfig 重排序配置
type RerankingConfig struct {
	Method            string `yaml:"method"`
	TopK              int    `yaml:"top_k"`
	UseGraphStructure bool   `yaml:"use_graph_structure"`
}

// GenerationConfig 生成配置
type GenerationConfig struct {
	Method          string  `yaml:"method"`
	MaxLength       int     `yaml:"max_length"`
	Temperature     float64 `yaml:"temperature"`
	IncludeSources  bool    `yaml:"include_sources"`
	UseGraphContext bool    `yaml:"use_graph_context"`
}

// LLMConfig LLM 配置
type LLMConfig struct {
	Provider string         `yaml:"provider"` // openai, azure, etc.
	APIKey   string         `yaml:"api_key"`
	BaseURL  string         `yaml:"base_url"`
	Model    string         `yaml:"model"`
	Options  map[string]any `yaml:"options"`
}

// EmbeddingConfig Embedding 配置
type EmbeddingConfig struct {
	Provider string         `yaml:"provider"`
	APIKey   string         `yaml:"api_key"`
	BaseURL  string         `yaml:"base_url"`
	Model    string         `yaml:"model"`
	Options  map[string]any `yaml:"options"`
}

// StorageConfig 存储配置
type StorageConfig struct {
	WorkingDir string `yaml:"working_dir"`
	TableName  string `yaml:"table_name"`
	GraphDB    string `yaml:"graph_db"`
}

// PromptStorageConfig 提示词存储配置（简化版，详细配置在 prompts.go 中）
type PromptStorageConfig struct {
	CustomDir string `yaml:"custom_dir"`
}

// ConfigManager 配置管理器
type ConfigManager struct {
	configPath string
	config     *Config
	envLoaded  bool
}

// NewConfigManager 创建配置管理器
func NewConfigManager(configPath string) *ConfigManager {
	return &ConfigManager{
		configPath: configPath,
		config:     DefaultConfig(),
	}
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		GraphRAG: GraphRAGConfig{
			Retrieval: RetrievalConfig{
				Limit:               10,
				MaxDepth:            2,
				SimilarityThreshold: 0.0,
				Strategy:            "hybrid",
			},
			Organization: OrganizationConfig{
				EnablePruning:       true,
				EnableReranking:     true,
				EnableAugmentation:  false,
				EnableVerbalization: true,
				Pruning: PruningConfig{
					MaxNodes:         50,
					MaxEdges:         100,
					MinScore:         0.3,
					KeepCoreEntities: true,
				},
				Reranking: RerankingConfig{
					Method:            "hybrid",
					TopK:              10,
					UseGraphStructure: true,
				},
			},
			Generation: GenerationConfig{
				Method:          "hybrid",
				MaxLength:       500,
				Temperature:     0.7,
				IncludeSources:  true,
				UseGraphContext: true,
			},
		},
		LLM: LLMConfig{
			Provider: "openai",
			Model:    "gpt-4",
		},
		Embedding: EmbeddingConfig{
			Provider: "openai",
			Model:    "text-embedding-ada-002",
		},
		Storage: StorageConfig{
			TableName: "graphsearch_entities",
			GraphDB:   "graphsearch.db",
		},
		Prompt: PromptStorageConfig{
			CustomDir: "./prompts",
		},
	}
}

// Load 加载配置
func (cm *ConfigManager) Load() error {
	// 先加载环境变量
	if err := cm.LoadEnv(); err != nil {
		return fmt.Errorf("failed to load environment variables: %w", err)
	}

	// 如果配置文件存在，加载配置文件
	if cm.configPath != "" {
		if _, err := os.Stat(cm.configPath); err == nil {
			// 配置文件存在，尝试加载（这里简化处理，实际应该使用 yaml 解析）
			// 由于 yaml 依赖问题，这里先使用简单的文本解析
			if err := cm.loadFromFile(); err != nil {
				return fmt.Errorf("failed to load config file: %w", err)
			}
		}
	}

	return nil
}

// LoadEnv 加载环境变量
func (cm *ConfigManager) LoadEnv() error {
	if cm.envLoaded {
		return nil
	}

	// 从环境变量加载配置
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		cm.config.LLM.APIKey = apiKey
		cm.config.Embedding.APIKey = apiKey
	}

	if baseURL := os.Getenv("OPENAI_BASE_URL"); baseURL != "" {
		cm.config.LLM.BaseURL = baseURL
		cm.config.Embedding.BaseURL = baseURL
	}

	if model := os.Getenv("LLM_MODEL"); model != "" {
		cm.config.LLM.Model = model
	}

	if embedModel := os.Getenv("EMBEDDING_MODEL"); embedModel != "" {
		cm.config.Embedding.Model = embedModel
	}

	if workingDir := os.Getenv("GRAPHSEARCH_WORKING_DIR"); workingDir != "" {
		cm.config.Storage.WorkingDir = workingDir
	}

	cm.envLoaded = true
	return nil
}

// loadFromFile 从文件加载配置（简化版，实际应该使用 yaml 解析）
func (cm *ConfigManager) loadFromFile() error {
	// 这里简化处理，实际应该使用 yaml.Unmarshal
	// 由于依赖问题，暂时跳过文件解析
	return nil
}

// Save 保存配置到文件
func (cm *ConfigManager) Save() error {
	if cm.configPath == "" {
		return fmt.Errorf("config path not set")
	}

	// 确保目录存在
	dir := filepath.Dir(cm.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 这里简化处理，实际应该使用 yaml.Marshal
	// 由于依赖问题，暂时跳过文件保存
	return nil
}

// GetConfig 获取配置
func (cm *ConfigManager) GetConfig() *Config {
	return cm.config
}

// UpdateConfig 更新配置
func (cm *ConfigManager) UpdateConfig(updater func(*Config)) {
	updater(cm.config)
}

// GetGraphRAGOptions 从配置生成 GraphRAGOptions
func (cm *ConfigManager) GetGraphRAGOptions() *GraphRAGOptions {
	config := cm.config

	return &GraphRAGOptions{
		RetrievalOptions: &RetrievalOptions{
			Limit:               config.GraphRAG.Retrieval.Limit,
			MaxDepth:            config.GraphRAG.Retrieval.MaxDepth,
			SimilarityThreshold: config.GraphRAG.Retrieval.SimilarityThreshold,
			Strategy:            RetrievalStrategy(config.GraphRAG.Retrieval.Strategy),
		},
		OrganizationOptions: &OrganizationOptions{
			EnablePruning:       config.GraphRAG.Organization.EnablePruning,
			EnableReranking:     config.GraphRAG.Organization.EnableReranking,
			EnableAugmentation:  config.GraphRAG.Organization.EnableAugmentation,
			EnableVerbalization: config.GraphRAG.Organization.EnableVerbalization,
			PruningOptions: &PruningOptions{
				MaxNodes:         config.GraphRAG.Organization.Pruning.MaxNodes,
				MaxEdges:         config.GraphRAG.Organization.Pruning.MaxEdges,
				MinScore:         config.GraphRAG.Organization.Pruning.MinScore,
				KeepCoreEntities: config.GraphRAG.Organization.Pruning.KeepCoreEntities,
			},
			RerankingOptions: &RerankingOptions{
				Method:            RerankingMethod(config.GraphRAG.Organization.Reranking.Method),
				TopK:              config.GraphRAG.Organization.Reranking.TopK,
				UseGraphStructure: config.GraphRAG.Organization.Reranking.UseGraphStructure,
			},
		},
		GenerationOptions: &GenerationOptions{
			Method:          GenerationMethod(config.GraphRAG.Generation.Method),
			MaxLength:       config.GraphRAG.Generation.MaxLength,
			Temperature:     config.GraphRAG.Generation.Temperature,
			IncludeSources:  config.GraphRAG.Generation.IncludeSources,
			UseGraphContext: config.GraphRAG.Generation.UseGraphContext,
		},
	}
}

// LoadConfigFromFile 从文件加载配置（辅助函数）
func LoadConfigFromFile(configPath string) (*Config, error) {
	manager := NewConfigManager(configPath)
	if err := manager.Load(); err != nil {
		return nil, err
	}
	return manager.GetConfig(), nil
}

// SaveConfigToFile 保存配置到文件（辅助函数）
func SaveConfigToFile(config *Config, configPath string) error {
	manager := NewConfigManager(configPath)
	manager.config = config
	return manager.Save()
}

// GetEnv 获取环境变量（辅助函数）
func GetEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// GetEnvBool 获取布尔环境变量（辅助函数）
func GetEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return strings.ToLower(value) == "true" || value == "1"
}

// GetEnvInt 获取整数环境变量（辅助函数）
func GetEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var result int
	if _, err := fmt.Sscanf(value, "%d", &result); err != nil {
		return defaultValue
	}
	return result
}
