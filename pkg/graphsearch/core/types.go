package graphsearch

import (
	"context"
	"time"
)

// Embedder 向量嵌入生成器接口
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
}

// Options graphsearch配置选项
type Options struct {
	Embedder   Embedder
	WorkingDir string // 工作目录，作为基础目录
	TableName  string // SQLite 表名，默认为 "graphsearch_entities"
}

// Entity GORM 模型，表示图谱存储中的实体
type Entity struct {
	EntityID        string    `gorm:"type:VARCHAR;primaryKey;not null;column:entity_id"`
	EntityName      string    `gorm:"type:TEXT;column:entity_name"`
	Metadata        string    `gorm:"type:TEXT;column:metadata"`  // JSON 格式的元数据
	Embedding       string    `gorm:"type:TEXT;column:embedding"` // JSON 格式的向量数组
	EmbeddingStatus string    `gorm:"type:VARCHAR;default:'pending';column:embedding_status"`
	CreatedAt       time.Time `gorm:"autoCreateTime;column:created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime;column:updated_at"`
}

// TableName 指定表名
func (Entity) TableName() string {
	return "graphsearch_entities"
}

// SemanticSearchResult 语义搜索结果
type SemanticSearchResult struct {
	EntityID   string
	EntityName string
	Score      float64
	Metadata   map[string]any
	Triples    []Triple // 与该实体相关的三元组
}

// Triple 表示图数据库中的三元组（subject-predicate-object）
type Triple struct {
	Subject   string
	Predicate string
	Object    string
}

// ========== GraphRAG 框架类型定义 ==========

// QueryProcessor 查询处理器接口
// 负责处理查询的预处理，包括实体识别、关系提取、查询结构化、查询分解、查询扩展
type QueryProcessor interface {
	// ProcessQuery 处理原始查询，返回结构化查询
	ProcessQuery(ctx context.Context, query string) (*StructuredQuery, error)
	// ExtractEntities 从查询中提取命名实体
	ExtractEntities(ctx context.Context, query string) ([]QueryEntity, error)
	// ExtractRelations 从查询中提取关系
	ExtractRelations(ctx context.Context, query string) ([]Relation, error)
	// DecomposeQuery 分解复杂查询为多个子查询
	DecomposeQuery(ctx context.Context, query string) ([]string, error)
	// ExpandQuery 扩展查询（同义词、相关概念等）
	ExpandQuery(ctx context.Context, query string) ([]string, error)
}

// StructuredQuery 结构化查询
type StructuredQuery struct {
	OriginalQuery string         // 原始查询
	Entities      []QueryEntity  // 提取的实体
	Relations     []Relation     // 提取的关系
	SubQueries    []string       // 分解的子查询
	ExpandedTerms []string       // 扩展的查询词
	Metadata      map[string]any // 查询元数据
}

// QueryEntity 查询中提取的实体（避免与 GORM Entity 模型冲突）
type QueryEntity struct {
	Name       string         // 实体名称
	Type       string         // 实体类型（如：Person, Organization, Location等）
	Confidence float64        // 置信度
	Metadata   map[string]any // 实体元数据
}

// Relation 查询中提取的关系
type Relation struct {
	Subject    string         // 主体
	Predicate  string         // 关系类型
	Object     string         // 客体
	Confidence float64        // 置信度
	Metadata   map[string]any // 关系元数据
}

// Retriever 检索器接口
// 负责从图中检索相关信息
type Retriever interface {
	// Retrieve 执行检索，返回相关实体和三元组
	Retrieve(ctx context.Context, query *StructuredQuery, options *RetrievalOptions) (*RetrievalResult, error)
	// HeuristicRetrieve 基于启发式的检索
	HeuristicRetrieve(ctx context.Context, query *StructuredQuery, options *RetrievalOptions) (*RetrievalResult, error)
	// LearningRetrieve 基于学习的检索（使用向量相似度）
	LearningRetrieve(ctx context.Context, query *StructuredQuery, options *RetrievalOptions) (*RetrievalResult, error)
}

// RetrievalOptions 检索选项
type RetrievalOptions struct {
	Limit               int               // 返回结果数量限制
	MaxDepth            int               // 图遍历最大深度
	SimilarityThreshold float64           // 相似度阈值
	UseHeuristic        bool              // 是否使用启发式检索
	UseLearning         bool              // 是否使用学习式检索
	Strategy            RetrievalStrategy // 检索策略
}

// RetrievalStrategy 检索策略
type RetrievalStrategy string

const (
	StrategyHeuristicOnly RetrievalStrategy = "heuristic_only" // 仅启发式
	StrategyLearningOnly  RetrievalStrategy = "learning_only"  // 仅学习式
	StrategyHybrid        RetrievalStrategy = "hybrid"         // 混合策略
	StrategyAdaptive      RetrievalStrategy = "adaptive"       // 自适应策略
)

// RetrievalResult 检索结果
type RetrievalResult struct {
	Entities   []RetrievedEntity // 检索到的实体
	Triples    []Triple          // 检索到的三元组
	Subgraphs  []Subgraph        // 检索到的子图
	Metadata   map[string]any    // 检索元数据
	TotalScore float64           // 总体相关性分数
}

// RetrievedEntity 检索到的实体
type RetrievedEntity struct {
	EntityID   string
	EntityName string
	Score      float64
	Metadata   map[string]any
	Triples    []Triple // 与该实体相关的三元组
}

// Subgraph 子图
type Subgraph struct {
	RootEntity string         // 根实体
	Triples    []Triple       // 子图中的三元组
	Entities   []string       // 子图中的实体ID列表
	Score      float64        // 子图相关性分数
	Metadata   map[string]any // 子图元数据
}

// Organizer 组织器接口
// 负责对检索结果进行组织、剪枝、重排序等处理
type Organizer interface {
	// Organize 组织检索结果
	Organize(ctx context.Context, result *RetrievalResult, options *OrganizationOptions) (*OrganizedResult, error)
	// PruneGraph 图剪枝
	PruneGraph(ctx context.Context, subgraphs []Subgraph, options *PruningOptions) ([]Subgraph, error)
	// Rerank 重排序
	Rerank(ctx context.Context, result *RetrievalResult, options *RerankingOptions) (*RetrievalResult, error)
	// AugmentGraph 图增强
	AugmentGraph(ctx context.Context, subgraphs []Subgraph, options *AugmentationOptions) ([]Subgraph, error)
	// Verbalize 将图结构转换为文本
	Verbalize(ctx context.Context, subgraphs []Subgraph, options *VerbalizationOptions) ([]string, error)
}

// OrganizationOptions 组织选项
type OrganizationOptions struct {
	EnablePruning        bool // 是否启用图剪枝
	EnableReranking      bool // 是否启用重排序
	EnableAugmentation   bool // 是否启用图增强
	EnableVerbalization  bool // 是否启用文本化
	PruningOptions       *PruningOptions
	RerankingOptions     *RerankingOptions
	AugmentationOptions  *AugmentationOptions
	VerbalizationOptions *VerbalizationOptions
}

// PruningOptions 剪枝选项
type PruningOptions struct {
	MaxNodes         int     // 最大节点数
	MaxEdges         int     // 最大边数
	MinScore         float64 // 最小分数阈值
	KeepCoreEntities bool    // 是否保留核心实体
}

// RerankingOptions 重排序选项
type RerankingOptions struct {
	Method            RerankingMethod // 重排序方法
	TopK              int             // 返回TopK结果
	UseGraphStructure bool            // 是否使用图结构信息
}

// RerankingMethod 重排序方法
type RerankingMethod string

const (
	RerankByScore      RerankingMethod = "score"      // 按分数排序
	RerankByCentrality RerankingMethod = "centrality" // 按中心性排序
	RerankByDiversity  RerankingMethod = "diversity"  // 按多样性排序
	RerankByHybrid     RerankingMethod = "hybrid"     // 混合排序
)

// AugmentationOptions 图增强选项
type AugmentationOptions struct {
	AddInferredRelations bool // 是否添加推理关系
	AddSimilarEntities   bool // 是否添加相似实体
	MaxAugmentations     int  // 最大增强数量
}

// VerbalizationOptions 文本化选项
type VerbalizationOptions struct {
	Format          VerbalizationFormat // 文本化格式
	IncludeMetadata bool                // 是否包含元数据
	MaxLength       int                 // 最大文本长度
}

// VerbalizationFormat 文本化格式
type VerbalizationFormat string

const (
	FormatNaturalLanguage VerbalizationFormat = "natural_language" // 自然语言
	FormatStructured      VerbalizationFormat = "structured"       // 结构化文本
	FormatSummary         VerbalizationFormat = "summary"          // 摘要格式
)

// OrganizedResult 组织后的结果
type OrganizedResult struct {
	Entities        []RetrievedEntity
	Triples         []Triple
	Subgraphs       []Subgraph
	VerbalizedTexts []string // 文本化后的文本
	Metadata        map[string]any
}

// Generator 生成器接口
// 负责基于检索和组织后的信息生成最终答案
type Generator interface {
	// Generate 生成答案
	Generate(ctx context.Context, organizedResult *OrganizedResult, query string, options *GenerationOptions) (*GeneratedAnswer, error)
	// GenerateWithLLM 使用LLM生成答案
	GenerateWithLLM(ctx context.Context, organizedResult *OrganizedResult, query string, options *GenerationOptions) (*GeneratedAnswer, error)
	// GenerateWithGraph 基于图结构生成答案
	GenerateWithGraph(ctx context.Context, organizedResult *OrganizedResult, query string, options *GenerationOptions) (*GeneratedAnswer, error)
}

// GenerationOptions 生成选项
type GenerationOptions struct {
	Method          GenerationMethod // 生成方法
	MaxLength       int              // 最大生成长度
	Temperature     float64          // 温度参数（用于LLM）
	IncludeSources  bool             // 是否包含来源信息
	UseGraphContext bool             // 是否使用图上下文
}

// GenerationMethod 生成方法
type GenerationMethod string

const (
	MethodDiscrimination GenerationMethod = "discrimination" // 基于判别的方法
	MethodLLM            GenerationMethod = "llm"            // 基于LLM的方法
	MethodGraph          GenerationMethod = "graph"          // 基于图的方法
	MethodHybrid         GenerationMethod = "hybrid"         // 混合方法
)

// GeneratedAnswer 生成的答案
type GeneratedAnswer struct {
	Answer     string         // 生成的答案文本
	Confidence float64        // 置信度
	Sources    []Source       // 答案来源
	Metadata   map[string]any // 元数据
}

// Source 答案来源
type Source struct {
	Type     string         // 来源类型（entity, triple, subgraph等）
	ID       string         // 来源ID
	Score    float64        // 相关性分数
	Metadata map[string]any // 来源元数据
}

// GraphRAGOptions GraphRAG完整配置选项
type GraphRAGOptions struct {
	QueryProcessor      QueryProcessor
	Retriever           Retriever
	Organizer           Organizer
	Generator           Generator
	RetrievalOptions    *RetrievalOptions
	OrganizationOptions *OrganizationOptions
	GenerationOptions   *GenerationOptions
}
