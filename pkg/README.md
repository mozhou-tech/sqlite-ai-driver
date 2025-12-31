# 存储驱动说明

本项目提供三个数据库存储驱动，它们都在配置的基础数据目录下使用各自的子目录进行数据存储。

## 目录结构

默认基础数据目录为 `./data`，目录结构如下：

```
./data/
├── cayley/         # cayley-driver 的数据目录
├── indexing/       # duckdb-driver 的共享数据库目录
└── db/             # sqlite3-driver 的数据目录
```

## 驱动详情

1. **cayley-driver**: 图数据库驱动
   - 后端: SQLite3
   - 数据目录: `{workingDir}/graph`（通过 WorkingDir 参数指定）
   - 用途: 图数据库存储

3. **duckdb-driver**: DuckDB扩展
   - 后端: DuckDB
   - 数据目录: `./data/indexing/index.db`（所有路径统一映射到此共享数据库）
   - 用途: DuckDB存储

4. **sqlite3-driver**: SQLite3业务数据
   - 后端: SQLite3
   - 数据目录: `./data/db`
   - 用途: SQLite3存储

## 自动目录设置

✅ **所有驱动现在都支持自动目录设置**。当使用相对路径（不包含路径分隔符）时，驱动会自动将数据文件存储到对应的子目录中。

### 自动目录行为

- **cayley-driver**: 相对路径（如 `"graph.db"`）自动存储到 `{workingDir}/graph/`（通过 WorkingDir 参数指定）
- **duckdb-driver**: 所有路径统一映射到共享数据库 `./data/indexing/index.db`
- **sqlite3-driver**: 相对路径（如 `"sqlite.db"`）自动存储到 `./data/db/`

### 数据目录配置

基础数据目录默认为 `./data`。对于需要自定义工作目录的场景（如 cayley-driver），可以通过 `WorkingDir` 参数指定。

## 使用示例

### 方式一：使用相对路径（推荐，自动目录设置）

使用相对路径时，驱动会自动将数据文件存储到对应的子目录中，无需手动构建路径：

```go
package main

import (
    "database/sql"
    
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
    cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
)

func main() {
    // 1. 使用 cayley-driver - 自动存储到 {workingDir}/graph/graph.db
    workingDir := "./data"
    graph, _ := cayley_driver.NewGraphWithNamespace(workingDir, "graph.db", "")
    defer graph.Close()
    
    // 2. 使用 duckdb-driver - 所有路径统一映射到 ./data/indexing/index.db
    duckDB, _ := sql.Open("duckdb", "duck.db")
    defer duckDB.Close()
    
    // 3. 使用 sqlite3-driver - 自动存储到 ./data/db/sqlite.db
    sqliteDB, _ := sql.Open("sqlite3", "sqlite.db")
    defer sqliteDB.Close()
}
```

### 方式二：使用完整路径（手动控制）

如果需要手动控制路径，可以使用完整路径：

```go
package main

import (
    "path/filepath"
    
    cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
)

func main() {
    dataDir := "/var/lib/myapp/data"
    
    // 使用 workingDir 和相对路径，会自动构建到 {dataDir}/graph/graph.db
    graph, _ := cayley_driver.NewGraphWithNamespace(dataDir, "graph.db", "")
    defer graph.Close()
}
```

## 路径处理规则

### 相对路径（自动目录设置）

当路径不包含路径分隔符（`/` 或 `\`）时，驱动会将其视为相对路径，自动构建到对应的子目录：

- `"graph.db"` → `{workingDir}/graph/graph.db`（通过 WorkingDir 参数指定）
- `"duck.db"` → `./data/indexing/index.db`（统一映射到共享数据库）
- `"sqlite.db"` → `./data/db/sqlite.db`

### 完整路径（手动控制）

当路径包含路径分隔符时，驱动会直接使用该路径，但仍会自动创建目录：

- `"./data/files.db"` → 直接使用，自动创建 `./data/` 目录
- `"/var/lib/myapp/data/files.db"` → 直接使用，自动创建 `/var/lib/myapp/data/` 目录

## 注意事项

1. **自动目录创建**: 所有驱动都会自动创建必要的目录，无需手动创建
2. **默认目录**: 默认使用 `./data` 作为基础数据目录
3. **WorkingDir 参数**: cayley-driver 通过 `WorkingDir` 参数指定工作目录，相对路径会构建到 `{workingDir}/graph/` 目录
4. **路径分隔符**: 使用 `filepath.Join()` 构建路径，确保跨平台兼容性
5. **权限设置**: 确保应用有读写数据目录的权限
6. **子目录约定**: 子目录名称（`graph`、`indexing`、`db`）是自动目录设置的约定，使用完整路径时可以自定义
7. **DuckDB 共享数据库**: duckdb-driver 将所有路径统一映射到 `./data/indexing/index.db` 共享数据库，不同业务模块通过表名前缀区分
