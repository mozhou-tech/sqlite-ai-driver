# Cayley 图数据库驱动

本模块提供了基于 SQLite3 的图数据库驱动，支持存储和查询图数据。

## 功能特性

- ✅ **图数据存储**：支持三元组（subject-predicate-object）存储
- ✅ **图查询 API**：提供类似 Gremlin 的查询接口（V, Out, In, All, Values）
- ✅ **路径查询**：支持查找两个节点之间的路径（BFS 算法）
- ✅ **SQLite3 后端**：使用 `github.com/mattn/go-sqlite3` 驱动
- ✅ **高性能索引**：自动创建索引优化查询性能
- ✅ **WAL 模式**：默认启用 WAL 模式提升并发性能

## 快速开始

### 安装依赖

**注意**：此驱动需要 CGO 支持，需要系统安装 SQLite3 开发库。

#### 系统依赖

- **macOS**: `brew install sqlite3`
- **Linux (Ubuntu/Debian)**: `sudo apt-get install libsqlite3-dev`
- **Linux (CentOS/RHEL)**: `sudo yum install sqlite-devel`

#### Go 依赖

```bash
go get github.com/mattn/go-sqlite3
```

### 基本使用

```go
package main

import (
    "context"
    "log"
    
    "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
)

func main() {
    ctx := context.Background()
    
    // 创建图数据库实例
    // workingDir 作为基础目录，相对路径会构建到 {workingDir}/graph/ 目录
    workingDir := "./data"
    graph, err := cayley_driver.NewGraphWithNamespace(workingDir, "graph.db", "")
    if err != nil {
        log.Fatal(err)
    }
    defer graph.Close()
    
    // 创建关系
    graph.Link(ctx, "user1", "follows", "user2")
    graph.Link(ctx, "user2", "follows", "user3")
    
    // 查询邻居
    neighbors, _ := graph.GetNeighbors(ctx, "user1", "follows")
    // neighbors: ["user2"]
    
    // 使用查询 API
    query := graph.Query()
    results, _ := query.V("user1").Out("follows").All(ctx)
    // results: [{Subject: "user1", Predicate: "follows", Object: "user2"}]
    
    // 查找路径
    paths, _ := graph.FindPath(ctx, "user1", "user3", 5, "follows")
    // paths: [["user1", "user2", "user3"]]
}
```

## API 文档

### Graph 接口

#### NewGraphWithNamespace(workingDir, path, namespace string) (Graph, error)

创建新的图数据库实例（支持表命名空间）。

- `workingDir`: 工作目录，作为基础目录，相对路径会构建到 {workingDir}/graph/ 目录
- `path`: SQLite3 数据库文件路径
  - 完整路径：/path/to/graph.db 或 ./path/to/graph.db
  - 相对路径（如 "graph.db"）：自动构建到 {workingDir}/graph/ 目录
- `namespace`: 表命名空间，如果为空则使用默认的 "quads" 表名
- 返回: Graph 实例和错误

#### Link(ctx, subject, predicate, object string) error

创建一条从 `subject` 到 `object` 的边，边的类型为 `predicate`。

#### Unlink(ctx, subject, predicate, object string) error

删除一条边。

#### GetNeighbors(ctx, node, predicate string) ([]string, error)

获取指定节点的邻居节点（出边）。

- `node`: 节点 ID
- `predicate`: 边的类型，如果为空则返回所有类型的邻居
- 返回: 邻居节点列表

#### GetInNeighbors(ctx, node, predicate string) ([]string, error)

获取指向指定节点的邻居节点（入边）。

#### Query() GraphQuery

返回查询构建器，支持类似 Gremlin 的查询语法。

#### FindPath(ctx, from, to string, maxDepth int, predicate string) ([][]string, error)

查找从 `from` 到 `to` 的路径。

- `from`: 起始节点
- `to`: 目标节点
- `maxDepth`: 最大深度
- `predicate`: 边的类型，如果为空则允许所有类型的边
- 返回: 路径列表，每条路径是一个节点序列

#### Close() error

关闭图数据库连接。

### GraphQuery 接口

#### V(node string) GraphQuery

选择指定的节点作为查询起点。

#### Out(predicate string) GraphQuery

沿着指定的边类型向外遍历（从 subject 到 object）。

#### In(predicate string) GraphQuery

沿着指定的边类型向内遍历（从 object 到 subject）。

#### All(ctx context.Context) ([]Triple, error)

执行查询并返回所有三元组结果。

#### Values(ctx context.Context) ([]string, error)

执行查询并返回所有节点值。

### Triple 结构

```go
type Triple struct {
    Subject   string  // 主体节点
    Predicate string  // 边的类型
    Object    string  // 客体节点
}
```

## 查询示例

### 基本查询

```go
// 查找 user1 关注的所有人
query := graph.Query()
results, _ := query.V("user1").Out("follows").All(ctx)

// 获取节点值
values, _ := query.V("user1").Out("follows").Values(ctx)
```

### 链式查询

```go
// A -> B -> C，查找 A 经过两步到达的节点
values, _ := graph.Query().V("A").Out("next").Out("next").Values(ctx)
```

### 入边查询

```go
// 查找关注 user2 的所有人
values, _ := graph.Query().V("user2").In("follows").Values(ctx)
```

### 路径查找

```go
// 查找从 user1 到 user3 的所有路径（最大深度 5）
paths, _ := graph.FindPath(ctx, "user1", "user3", 5, "follows")
```

## 数据库结构

图数据存储在 SQLite3 的 `quads` 表中：

```sql
CREATE TABLE quads (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subject TEXT NOT NULL,
    predicate TEXT NOT NULL,
    object TEXT NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(subject, predicate, object)
);
```

自动创建的索引：
- `idx_quads_subject`: 按 subject 查询
- `idx_quads_predicate`: 按 predicate 查询
- `idx_quads_object`: 按 object 查询
- `idx_quads_sp`: 按 (subject, predicate) 查询
- `idx_quads_po`: 按 (predicate, object) 查询

## 性能优化

- 使用 WAL 模式提升并发读写性能
- 自动创建索引优化查询
- 支持批量操作（通过事务）

## 注意事项

- 使用 `github.com/mattn/go-sqlite3` 驱动，需要 CGO 支持
- 需要系统安装 SQLite3 开发库（详见安装说明）
- 默认启用 WAL 模式
- 三元组自动去重（通过 UNIQUE 约束）
- 路径查找使用 BFS 算法，限制最大返回 100 条路径

## 示例代码

完整示例请参考 `examples/cayley-graph/main.go`。



