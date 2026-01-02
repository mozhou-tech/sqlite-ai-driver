# 向量检索 Graph 实体使用指南

## 概述

GraphStore 的向量检索功能允许你通过自然语言查询找到相关的图谱实体，并自动返回这些实体在图谱中的关系。向量检索使用 DuckDB 的 `index.db` 共享数据库进行高效的向量相似度搜索。

## 快速开始

### 1. 基本使用

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/mozhou-tech/sqlite-ai-driver/pkg/graphstore"
)

func main() {
    ctx := context.Background()
    
    // 1. 创建 Embedder（需要实现 graphstore.Embedder 接口）
    embedder := &YourEmbedder{}
    
    // 2. 创建并初始化 GraphStore
    store, err := graphstore.New(graphstore.Options{
        Embedder:   embedder,
        WorkingDir: "./data", // 工作目录，作为基础目录
    })
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    
    if err := store.Initialize(ctx); err != nil {
        log.Fatal(err)
    }
    
    // 3. 添加实体（会自动生成 embedding）
    store.AddEntity(ctx, "person1", "张三", map[string]any{"job": "软件工程师"})
    store.AddEntity(ctx, "person2", "李四", map[string]any{"job": "产品经理"})
    
    // 4. 创建关系
    store.Link(ctx, "person1", "knows", "person2")
    
    // 5. 向量检索
    results, err := store.SemanticSearch(ctx, "软件工程师", 5, 2)
    if err != nil {
        log.Fatal(err)
    }
    
    // 6. 处理结果
    for _, result := range results {
        fmt.Printf("找到实体: %s (相似度: %.4f)\n", result.EntityName, result.Score)
        fmt.Printf("相关关系: %d 条\n", len(result.Triples))
    }
}
```

## 核心方法：SemanticSearch

### 方法签名

```go
func (g *GraphStore) SemanticSearch(
    ctx context.Context, 
    query string,      // 查询文本
    limit int,         // 返回的实体数量限制
    maxDepth int,      // 图谱遍历的最大深度
) ([]SemanticSearchResult, error)
```

### 参数说明

- **query** (string): 查询文本，会被 Embedder 转换为向量进行搜索
- **limit** (int): 返回的实体数量限制，默认 10
- **maxDepth** (int): 为每个找到的实体获取其周围深度为 `maxDepth` 的子图，默认 2

### 返回值

```go
type SemanticSearchResult struct {
    EntityID   string              // 实体ID
    EntityName string              // 实体名称
    Score      float64             // 相似度分数（余弦相似度，范围 [0, 1]）
    Metadata   map[string]any      // 实体的元数据
    Triples    []Triple            // 该实体在图谱中的关系（子图）
}

type Triple struct {
    Subject   string  // 主体
    Predicate string  // 关系类型
    Object    string  // 客体
}
```

## 工作流程

### 步骤 1: 添加实体并生成 Embedding

```go
// AddEntity 会自动调用 Embedder 生成 embedding
// embedding 存储在 DuckDB 的 index.db 数据库中
store.AddEntity(ctx, "entity1", "机器学习专家", map[string]any{
    "skill": "ML",
    "level": "senior",
})
```

### 步骤 2: 创建图谱关系

```go
// 创建实体之间的关系
store.Link(ctx, "entity1", "works_at", "company1")
store.Link(ctx, "entity1", "knows", "entity2")
```

### 步骤 3: 执行向量检索

```go
// 查询: "AI技术专家"
// 返回: 前 5 个最相似的实体
// 深度: 获取每个实体周围深度为 2 的子图
results, err := store.SemanticSearch(ctx, "AI技术专家", 5, 2)
```

### 步骤 4: 处理检索结果

```go
for _, result := range results {
    // 基本信息
    fmt.Printf("实体: %s\n", result.EntityName)
    fmt.Printf("相似度: %.4f\n", result.Score)
    
    // 元数据
    if job, ok := result.Metadata["job"].(string); ok {
        fmt.Printf("职位: %s\n", job)
    }
    
    // 图谱关系
    fmt.Printf("相关关系 (%d 条):\n", len(result.Triples))
    for _, triple := range result.Triples {
        fmt.Printf("  %s --[%s]--> %s\n", 
            triple.Subject, triple.Predicate, triple.Object)
    }
}
```

## 向量检索原理

### 1. 向量存储

- 实体添加时，`AddEntity` 调用 `Embedder.Embed()` 生成 embedding
- Embedding 存储在 DuckDB 的 `index.db` 共享数据库中
- 表名默认为 `graphstore_entities`，可通过 `Options.TableName` 自定义

### 2. 向量搜索

- `SemanticSearch` 将查询文本转换为向量
- 使用 DuckDB 的 `list_cosine_similarity` 函数计算相似度
- SQL 查询：
  ```sql
  SELECT entity_id, entity_name, metadata,
         list_cosine_similarity(embedding, ?::FLOAT[]) as similarity
  FROM graphstore_entities
  WHERE embedding IS NOT NULL AND embedding_status = 'completed'
  ORDER BY similarity DESC
  LIMIT ?
  ```

### 3. 图谱扩展

- 为每个找到的实体，使用 BFS 算法获取其周围深度为 `maxDepth` 的子图
- 返回所有相关的三元组（关系）

## 使用场景

### 场景 1: 查找相关人员

```go
// 查找"软件工程师"相关的人员
results, _ := store.SemanticSearch(ctx, "软件工程师", 10, 2)

// 结果包含：
// - 所有职位为"软件工程师"或语义相似的人员
// - 这些人员在公司、项目、社交网络中的关系
```

### 场景 2: 查找公司信息

```go
// 查找"科技公司"相关的公司
results, _ := store.SemanticSearch(ctx, "科技公司", 5, 1)

// 结果包含：
// - 所有科技公司
// - 这些公司的人员、项目等一度关系
```

### 场景 3: 知识图谱问答

```go
// 查询: "谁在负责AI项目？"
results, _ := store.SemanticSearch(ctx, "AI项目负责人", 5, 3)

// 结果包含：
// - 与"AI项目负责人"语义相似的实体
// - 这些实体的完整上下文关系（三度关系）
```

## 实现 Embedder 接口

你需要实现 `Embedder` 接口来生成 embedding：

```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float64, error)
    Dimensions() int
}
```

### 示例：使用 OpenAI Embedding

```go
import (
    "context"
    "github.com/cloudwego/eino-ext/components/embedding/openai"
)

type OpenAIEmbedder struct {
    embedder *openai.Embedder
    dims     int
}

func NewOpenAIEmbedder(ctx context.Context, apiKey string) (*OpenAIEmbedder, error) {
    config := &openai.EmbeddingConfig{
        APIKey: apiKey,
        Model:  "text-embedding-3-small", // 或 "text-embedding-3-large"
    }
    
    emb, err := openai.NewEmbedder(ctx, config)
    if err != nil {
        return nil, err
    }
    
    return &OpenAIEmbedder{
        embedder: emb,
        dims:     1536, // text-embedding-3-small 的维度
    }, nil
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
    vectors, err := e.embedder.EmbedStrings(ctx, []string{text})
    if err != nil {
        return nil, err
    }
    if len(vectors) == 0 {
        return nil, fmt.Errorf("no embedding returned")
    }
    return vectors[0], nil
}

func (e *OpenAIEmbedder) Dimensions() int {
    return e.dims
}
```

## 注意事项

1. **初始化顺序**：必须先调用 `Initialize()` 才能使用向量检索
2. **Embedding 状态**：只有 `embedding_status = 'completed'` 的实体会被检索
3. **性能考虑**：
   - `limit` 越大，返回结果越多，但计算时间越长
   - `maxDepth` 越大，子图越大，但能提供更完整的上下文
4. **向量维度**：确保所有实体的 embedding 维度一致
5. **数据库位置**：向量数据存储在 `./data/indexing/index.db`，由 `sqlite-driver` 自动管理

## 完整示例

参考 `example_test.go` 文件中的完整示例代码。

