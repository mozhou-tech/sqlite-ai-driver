# GraphStore

基于 `cayley-driver` 和 `duckdb-driver` 的纯图谱存储系统，支持实体 embedding 存储和语义检索图谱。

## 功能特性

- **图谱存储**：使用 `cayley-driver` 存储图谱关系（三元组）
- **实体 Embedding**：使用 `duckdb-driver` 存储实体的 embedding 信息
- **语义检索**：通过查询文本找到相似的实体，然后返回这些实体在图谱中的关系
- **子图查询**：支持获取实体的子图（指定深度内的所有关系）

## 使用方法

### 1. 创建 GraphStore 实例

```go
import (
    "context"
    "github.com/mozhou-tech/sqlite-ai-driver/pkg/graphstore"
)

// 创建 embedder（需要实现 Embedder 接口）
embedder := &YourEmbedder{}

// 创建 GraphStore
store, err := graphstore.New(graphstore.Options{
    Embedder:   embedder,
    WorkingDir: "./data",         // 工作目录，作为基础目录（必填）
    GraphDB:    "graphstore.db",  // 图谱数据库路径（可选，默认 "graphstore.db"）
    TableName:  "graphstore_entities", // DuckDB 表名（可选，默认 "graphstore_entities"）
})
if err != nil {
    log.Fatal(err)
}
defer store.Close()

// 初始化存储
ctx := context.Background()
if err := store.Initialize(ctx); err != nil {
    log.Fatal(err)
}
```

### 2. 添加实体

```go
// 添加实体及其 embedding
err := store.AddEntity(ctx, "entity1", "实体名称", map[string]any{
    "type": "person",
    "age":  30,
})
```

### 3. 创建关系

```go
// 创建关系（三元组）
err := store.Link(ctx, "entity1", "knows", "entity2")
err = store.Link(ctx, "entity2", "works_at", "company1")
```

### 4. 向量检索图谱实体（语义检索）

向量检索是 GraphStore 的核心功能，它通过以下步骤工作：

1. **生成查询向量**：使用 Embedder 将查询文本转换为向量
2. **向量相似度搜索**：在 DuckDB 的 `index.db` 数据库中，使用 `list_cosine_similarity` 函数查找最相似的实体
3. **获取图谱关系**：为每个找到的实体获取其在图谱中的关系（子图）

```go
// 语义检索图谱
// 参数说明：
//   - query: 查询文本（会被转换为向量进行搜索）
//   - limit: 返回的实体数量限制（默认 10）
//   - maxDepth: 图谱遍历的最大深度，用于获取每个实体的子图（默认 2）
results, err := store.SemanticSearch(ctx, "查询文本", 10, 2)
if err != nil {
    log.Fatal(err)
}

for _, result := range results {
    fmt.Printf("实体: %s (ID: %s)\n", result.EntityName, result.EntityID)
    fmt.Printf("相似度分数: %.4f\n", result.Score)  // 余弦相似度，范围 [0, 1]
    fmt.Printf("元数据: %v\n", result.Metadata)
    fmt.Printf("相关三元组数量: %d\n", len(result.Triples))
    
    // 显示该实体在图谱中的所有关系
    for _, triple := range result.Triples {
        fmt.Printf("  %s --[%s]--> %s\n", triple.Subject, triple.Predicate, triple.Object)
    }
}
```

**向量检索的工作原理**：

1. **向量存储**：实体添加时，`AddEntity` 会自动调用 Embedder 生成 embedding 并存储到 DuckDB
2. **向量搜索**：`SemanticSearch` 使用 DuckDB 的 `list_cosine_similarity` 函数计算查询向量与所有实体向量的相似度
3. **结果排序**：按相似度从高到低排序，返回 top-K 个实体
4. **图谱扩展**：为每个找到的实体，获取其在图谱中深度为 `maxDepth` 的子图关系

**示例场景**：

```go
// 场景1: 查找"软件工程师"相关的人员
results, _ := store.SemanticSearch(ctx, "软件工程师", 5, 2)
// 返回：所有实体名称或元数据中包含"软件工程师"语义的实体及其关系

// 场景2: 查找特定公司的人员
results, _ := store.SemanticSearch(ctx, "科技公司A的员工", 10, 1)
// 返回：与"科技公司A"语义相似的实体，以及它们的一度关系

// 场景3: 查找项目相关的人员和公司
results, _ := store.SemanticSearch(ctx, "AI项目", 5, 3)
// 返回：与"AI项目"相关的实体，以及它们的三度关系（更完整的上下文）
```

### 5. 获取子图

```go
// 获取实体的子图（指定深度内的所有关系）
triples, err := store.GetSubgraph(ctx, "entity1", 2)
if err != nil {
    log.Fatal(err)
}

for _, triple := range triples {
    fmt.Printf("%s --[%s]--> %s\n", triple.Subject, triple.Predicate, triple.Object)
}
```

### 6. 其他操作

```go
// 获取邻居节点
neighbors, err := store.GetNeighbors(ctx, "entity1", "knows")

// 获取入边邻居
inNeighbors, err := store.GetInNeighbors(ctx, "entity1", "knows")

// 查找路径
paths, err := store.FindPath(ctx, "entity1", "entity2", 5, "")

// 获取实体信息
entity, err := store.GetEntity(ctx, "entity1")

// 列出所有实体
entities, err := store.ListEntities(ctx, 100, 0)

// 使用查询构建器
query := store.Query()
triples, err := query.V("entity1").Out("knows").All(ctx)
```

## Embedder 接口

需要实现 `Embedder` 接口来生成 embedding：

```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float64, error)
    Dimensions() int
}
```

## 数据存储

- **图谱数据**：存储在 SQLite 数据库中（通过 `cayley-driver`）
- **实体 Embedding（向量检索）**：存储在 DuckDB 数据库中（通过 `duckdb-driver`）
  - 向量检索使用 `duckdb-driver` 的 `index.db` 共享数据库
  - 所有 DuckDB 数据统一映射到 `./data/indexing/index.db`
  - 不同的业务模块通过表名区分（如 `graphstore_entities`）

## 注意事项

1. 调用 `Initialize()` 之前，GraphStore 未初始化，大部分操作会返回错误
2. 如果提供了 `Embedder`，`AddEntity` 会自动生成 embedding
3. `SemanticSearch` 只返回有 embedding 的实体（`embedding_status = 'completed'`）
4. 子图查询使用 BFS 算法，深度从 0 开始计算

