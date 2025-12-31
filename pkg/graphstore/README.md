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
    Embedder:  embedder,
    GraphDB:   "graphstore.db",  // 图谱数据库路径（可选，默认 "graphstore.db"）
    TableName: "graphstore_entities", // DuckDB 表名（可选，默认 "graphstore_entities"）
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

### 4. 语义检索

```go
// 语义检索图谱
// 通过查询文本找到相似的实体，然后返回这些实体在图谱中的关系
results, err := store.SemanticSearch(ctx, "查询文本", 10, 2)
if err != nil {
    log.Fatal(err)
}

for _, result := range results {
    fmt.Printf("实体: %s (相似度: %.4f)\n", result.EntityName, result.Score)
    fmt.Printf("元数据: %v\n", result.Metadata)
    fmt.Printf("相关三元组数量: %d\n", len(result.Triples))
    for _, triple := range result.Triples {
        fmt.Printf("  %s --[%s]--> %s\n", triple.Subject, triple.Predicate, triple.Object)
    }
}
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
- **实体 Embedding**：存储在 DuckDB 数据库中（通过 `duckdb-driver`）
  - 所有 DuckDB 数据统一映射到 `{DATA_DIR}/indexing/all.db`
  - 使用表名区分不同的业务模块

## 注意事项

1. 调用 `Initialize()` 之前，GraphStore 未初始化，大部分操作会返回错误
2. 如果提供了 `Embedder`，`AddEntity` 会自动生成 embedding
3. `SemanticSearch` 只返回有 embedding 的实体（`embedding_status = 'completed'`）
4. 子图查询使用 BFS 算法，深度从 0 开始计算

