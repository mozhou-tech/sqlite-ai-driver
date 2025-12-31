# CURSOR_RULE - sqlite-ai-driver 依赖使用指南

当在其他仓库中将本工程的 `pkg` 目录作为依赖库使用时，请遵循以下规则和最佳实践。

## 📦 模块导入路径

本项目的模块路径为：`github.com/mozhou-tech/sqlite-ai-driver`

### 可用包列表

1. **数据库驱动包**（database/sql 驱动）：
   - `github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver` - DuckDB 驱动
   - `github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver` - SQLite3 驱动

2. **图数据库包**：
   - `github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver` - 图数据库驱动（独立 API）

3. **Eino 扩展包**：
   - `github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/indexer/duckdb` - DuckDB 索引器
   - `github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/indexer/lightrag` - LightRAG 索引器
   - `github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/retriever/vec` - DuckDB 检索器（包名：duckdb）
   - `github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/retriever/lightrag` - LightRAG 检索器
   - `github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/document/transformer/splitter/tfidf` - TF-IDF 文档分割器

## 🔧 安装依赖

### 1. 添加依赖到 go.mod

```bash
go get github.com/mozhou-tech/sqlite-ai-driver
go mod tidy
```


#### DuckDB Driver

DuckDB 驱动会自动安装所需的扩展（sqlite, vss, fts, excel），无需额外配置。

## ⚙️ 数据目录配置

### 默认数据目录

所有驱动默认使用 `./data` 作为基础数据目录。

### 数据目录结构

当使用相对路径时，驱动会自动将数据文件存储到对应的子目录：

```
./data/
├── graph/          # cayley-driver 的数据目录（通过 WorkingDir 参数指定）
├── indexing/       # duckdb-driver 的共享数据库目录
└── db/             # sqlite3-driver 的数据目录
```

## 📝 使用示例

### 1. 数据库驱动使用（database/sql）

#### DuckDB Driver

```go
import (
    "database/sql"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

// 使用相对路径（推荐）- 所有路径统一映射到 ./data/indexing/index.db
db, err := sql.Open("duckdb", "duck.db")

// 使用完整路径
db, err := sql.Open("duckdb", "/path/to/duck.db")
```

#### SQLite3 Driver

```go
import (
    "database/sql"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
)

// 使用相对路径（推荐）- 自动存储到 ./data/db/sqlite.db
db, err := sql.Open("sqlite3", "sqlite.db")

// 使用完整路径
db, err := sql.Open("sqlite3", "/path/to/sqlite.db")
```

### 2. 图数据库使用（Cayley Driver）

```go
import (
    "context"
    cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
)

// 创建图数据库实例
// workingDir 作为基础目录，相对路径会构建到 {workingDir}/graph/ 目录
workingDir := "./data"
graph, err := cayley_driver.NewGraphWithNamespace(workingDir, "graph.db", "") // 自动存储到 {workingDir}/graph/graph.db
if err != nil {
    log.Fatal(err)
}
defer graph.Close()

ctx := context.Background()

// 创建关系
graph.Link(ctx, "user1", "follows", "user2")

// 查询邻居
neighbors, _ := graph.GetNeighbors(ctx, "user1", "follows")

// 使用查询 API
query := graph.Query()
results, _ := query.V("user1").Out("follows").All(ctx)
```

### 3. Eino 扩展使用

#### DuckDB Indexer

```go
import (
    "context"
    "database/sql"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
    duckdbindexer "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/indexer/duckdb"
)

// 打开 DuckDB 连接
db, _ := sql.Open("duckdb", "duck.db")
defer db.Close()

// 创建索引器
indexer, err := duckdbindexer.NewIndexer(ctx, &duckdbindexer.IndexerConfig{
    DB:        db,
    TableName: "documents",
    Embedding: embeddingClient, // 需要提供 embedding.Embedder 实例
})
```

#### DuckDB Retriever

```go
import (
    "context"
    "database/sql"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
    duckdbretriever "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/retriever/vec"
)

// 打开 DuckDB 连接
db, _ := sql.Open("duckdb", "duck.db")
defer db.Close()

// 创建检索器
retriever, err := duckdbretriever.NewRetriever(ctx, &duckdbretriever.RetrieverConfig{
    DB:        db,
    TableName: "documents",
    Embedding: embeddingClient,
    TopK:      5,
})
```

## 🛣️ 路径处理规则

### 相对路径（自动目录设置）

当路径**不包含路径分隔符**（`/` 或 `\`）时，驱动会将其视为相对路径，自动构建到对应的子目录：

- `"graph.db"` → `{workingDir}/graph/graph.db`（通过 WorkingDir 参数指定）
- `"duck.db"` → `./data/indexing/index.db`（统一映射到共享数据库）
- `"sqlite.db"` → `./data/db/sqlite.db`

### 完整路径（手动控制）

当路径**包含路径分隔符**时，驱动会直接使用该路径，但仍会自动创建目录：

- `"./data/files.db"` → 直接使用，自动创建 `./data/` 目录
- `"/var/lib/myapp/data/files.db"` → 直接使用，自动创建 `/var/lib/myapp/data/` 目录

## ✅ 最佳实践

### 1. 使用相对路径（推荐）

使用相对路径可以让驱动自动管理目录结构，代码更简洁：

```go
// ✅ 推荐：使用相对路径
db, _ := sql.Open("sqlite3", "app.db")

// ❌ 不推荐：手动构建路径（除非有特殊需求）
db, _ := sql.Open("sqlite3", filepath.Join("./data", "db", "app.db"))
```

### 2. 确保目录权限

确保应用有读写数据目录的权限：

```go
// 在应用启动时检查并创建目录
dataDir := "./data"
if err := os.MkdirAll(dataDir, 0755); err != nil {
    log.Fatal(err)
}
```

### 3. 跨平台路径处理

使用 `filepath.Join()` 构建路径，确保跨平台兼容性：

```go
import "path/filepath"

// ✅ 正确
path := filepath.Join(dataDir, "db", "app.db")

// ❌ 错误（硬编码路径分隔符）
path := dataDir + "/db/app.db"
```

## ⚠️ 注意事项

### 1. 驱动注册

所有数据库驱动都通过 `init()` 函数自动注册，只需导入即可：

```go
// ✅ 正确：使用空白导入
import _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"

// ❌ 错误：不要直接导入包（除非需要使用包内的其他函数）
import "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
```

### 2. 连接管理

- 所有数据库连接都需要在使用完毕后关闭（`defer db.Close()`）
- Cayley Graph 实例也需要关闭（`defer graph.Close()`）
- 不要在多个 goroutine 之间共享未加锁的数据库连接

### 3. 并发安全

- SQLite 驱动支持并发读取，但写入需要加锁
- 使用 `database/sql` 包的连接池管理并发连接
- Cayley Driver 的 Graph 实例不是并发安全的，需要在应用层加锁

### 4. DuckDB 扩展

DuckDB Driver 会自动安装以下扩展：

- `sqlite` - SQLite 扩展
- `vss` - 向量搜索扩展
- `fts` - 全文搜索扩展
- `excel` - Excel 扩展

首次使用时可能需要下载扩展，确保网络连接正常。

## 🔍 故障排查

### 问题：找不到驱动

**症状**：`sql: unknown driver "file"`

**解决方案**：
```go
// 确保已导入驱动
import _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
```

### 问题：CGO 相关错误（Cayley Driver）

**症状**：`#cgo` 相关编译错误

**解决方案**：
```bash
# 确保 CGO 已启用
export CGO_ENABLED=1

# 确保已安装 SQLite3 开发库
# macOS: brew install sqlite3
# Linux: sudo apt-get install libsqlite3-dev
```

### 问题：权限错误

**症状**：`permission denied` 或 `read-only file system`

**解决方案**：
- 检查数据目录的读写权限
- 确保应用有创建目录的权限
- 检查磁盘空间是否充足

### 问题：路径解析错误

**症状**：文件未存储到预期位置

**解决方案**：
- 检查数据目录权限和路径是否正确
- 确认路径格式（相对路径 vs 完整路径）
- 查看驱动日志（如果启用）

## 📚 相关文档

- 项目 README: `pkg/README.md`
- Cayley Driver 文档: `pkg/cayley-driver/README.md`
- Cayley Driver 安装说明: `pkg/cayley-driver/INSTALL.md`

## 🔗 相关链接

- 项目仓库: `github.com/mozhou-tech/sqlite-ai-driver`
- Eino 框架: `github.com/cloudwego/eino`
- DuckDB: `https://duckdb.org/`
- SQLite: `https://www.sqlite.org/`

