# 存储驱动说明

本项目提供四个数据库和文件的存储驱动，它们都在配置的基础数据目录下使用各自的子目录进行数据存储。

## 目录结构

假设配置的基础数据目录为 `{data_dir}`，则目录结构如下：

```
{data_dir}/
├── files/          # file-driver 的数据目录
├── cayley/         # cayley-driver 的数据目录
├── duck/           # duckdb-driver 的数据目录
└── db/             # sqlite3-driver 的数据目录
```

## 驱动详情

1. **file-driver**: 文件驱动
   - 后端: SQLite3
   - 数据目录: `{data_dir}/files`
   - 用途: 文件存储

2. **cayley-driver**: 图数据库驱动
   - 后端: SQLite3
   - 数据目录: `{data_dir}/cayley`
   - 用途: 图数据库存储

3. **duckdb-driver**: DuckDB扩展
   - 后端: SQLite3
   - 数据目录: `{data_dir}/duck`
   - 用途: DuckDB存储

4. **sqlite3-driver**: SQLite3业务数据
   - 后端: SQLite3
   - 数据目录: `{data_dir}/db`
   - 用途: SQLite3存储

## 自动目录设置

✅ **所有驱动现在都支持自动目录设置**。当使用相对路径（不包含路径分隔符）时，驱动会自动将数据文件存储到对应的子目录中。

### 自动目录行为

- **file-driver**: 相对路径（如 `"files.db"`）自动存储到 `{DATA_DIR}/files/`
- **cayley-driver**: 相对路径（如 `"graph.db"`）自动存储到 `{DATA_DIR}/cayley/`
- **duckdb-driver**: 相对路径（如 `"duck.db"`）自动存储到 `{DATA_DIR}/duck/`
- **sqlite3-driver**: 相对路径（如 `"sqlite.db"`）自动存储到 `{DATA_DIR}/db/`

### 数据目录配置

基础数据目录 `{DATA_DIR}` 可以通过环境变量 `DATA_DIR` 设置，默认值为 `./data`。

```bash
# 设置数据目录
export DATA_DIR=/var/lib/myapp/data
```

如果未设置 `DATA_DIR` 环境变量，默认使用 `./data` 作为基础目录。

## 使用示例

### 方式一：使用相对路径（推荐，自动目录设置）

使用相对路径时，驱动会自动将数据文件存储到对应的子目录中，无需手动构建路径：

```go
package main

import (
    "database/sql"
    
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/file-driver"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
    cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
)

func main() {
    // 设置数据目录（可选，默认使用 ./data）
    // os.Setenv("DATA_DIR", "/var/lib/myapp/data")
    
    // 1. 使用 file-driver - 自动存储到 {DATA_DIR}/files/files.db
    fileDB, _ := sql.Open("file", "files.db")
    defer fileDB.Close()
    
    // 2. 使用 cayley-driver - 自动存储到 {workingDir}/graph/graph.db
    workingDir := "./data"
    graph, _ := cayley_driver.NewGraphWithPrefix(workingDir, "graph.db", "")
    defer graph.Close()
    
    // 3. 使用 duckdb-driver - 自动存储到 {DATA_DIR}/duck/duck.db
    duckDB, _ := sql.Open("duckdb", "duck.db")
    defer duckDB.Close()
    
    // 4. 使用 sqlite3-driver - 自动存储到 {DATA_DIR}/db/sqlite.db
    sqliteDB, _ := sql.Open("sqlite3", "sqlite.db")
    defer sqliteDB.Close()
}
```

### 方式二：使用完整路径（手动控制）

如果需要手动控制路径，可以使用完整路径：

```go
package main

import (
    "database/sql"
    "path/filepath"
    
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/file-driver"
    cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
)

func main() {
    dataDir := "/var/lib/myapp/data"
    
    // 使用完整路径，驱动会自动创建目录
    fileDB, _ := sql.Open("file", filepath.Join(dataDir, "files", "files.db"))
    defer fileDB.Close()
    
    // 使用 workingDir 和相对路径，会自动构建到 {dataDir}/graph/graph.db
    graph, _ := cayley_driver.NewGraphWithPrefix(dataDir, "graph.db", "")
    defer graph.Close()
}
```

### 方式三：通过环境变量配置

```bash
# 设置数据目录
export DATA_DIR=/var/lib/myapp/data

# 运行程序
go run main.go
```

在代码中直接使用相对路径，驱动会自动使用 `DATA_DIR` 环境变量：

```go
// 无需手动设置路径，驱动会自动从环境变量读取 DATA_DIR
db, _ := sql.Open("file", "files.db")  // 自动存储到 $DATA_DIR/files/files.db
```

## 路径处理规则

### 相对路径（自动目录设置）

当路径不包含路径分隔符（`/` 或 `\`）时，驱动会将其视为相对路径，自动构建到对应的子目录：

- `"files.db"` → `{DATA_DIR}/files/files.db`
- `"graph.db"` → `{DATA_DIR}/cayley/graph.db`
- `"duck.db"` → `{DATA_DIR}/duck/duck.db`
- `"sqlite.db"` → `{DATA_DIR}/db/sqlite.db`

### 完整路径（手动控制）

当路径包含路径分隔符时，驱动会直接使用该路径，但仍会自动创建目录：

- `"./data/files.db"` → 直接使用，自动创建 `./data/` 目录
- `"/var/lib/myapp/data/files.db"` → 直接使用，自动创建 `/var/lib/myapp/data/` 目录

## 注意事项

1. **自动目录创建**: 所有驱动都会自动创建必要的目录，无需手动创建
2. **环境变量**: 通过 `DATA_DIR` 环境变量可以统一配置基础数据目录
3. **路径分隔符**: 使用 `filepath.Join()` 构建路径，确保跨平台兼容性
4. **权限设置**: 确保应用有读写数据目录的权限
5. **子目录约定**: 子目录名称（`files`、`cayley`、`duck`、`db`）是自动目录设置的约定，使用完整路径时可以自定义
