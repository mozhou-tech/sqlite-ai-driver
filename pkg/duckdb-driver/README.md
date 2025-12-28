# DuckDB Driver - 支持 Sego 中文分词

本驱动提供了对 DuckDB 数据库的支持，并集成了 Sego 中文分词功能，用于提升中文全文搜索的准确性。

## 功能特性

- ✅ **DuckDB 数据库驱动**：支持标准的 `database/sql` 接口
- ✅ **自动扩展加载**：自动安装和加载 FTS、VSS、SQLite、Excel 等扩展
- ✅ **Sego 中文分词**：集成 Sego 分词器，支持中文文本分词
- ✅ **全文搜索增强**：支持基于分词结果的全文搜索

## 快速开始

### 基本使用

```go
package main

import (
    "database/sql"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

func main() {
    // 打开数据库连接
    db, err := sql.Open("duckdb", "my_database.db")
    if err != nil {
        panic(err)
    }
    defer db.Close()
    
    // 使用数据库...
}
```

### 使用 Sego 中文分词

#### 1. 分词文本

```go
import (
    "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

// 对文本进行分词
text := "这是一个中文文本"
tokens := duckdb_driver.TokenizeWithSego(text)
// tokens: "这是 一个 中文 文本"
```

#### 2. 创建支持分词的 FTS 索引

```go
import (
    "context"
    "database/sql"
    "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

func createTableWithFTS(ctx context.Context, db *sql.DB) error {
    // 创建表
    _, err := db.ExecContext(ctx, `
        CREATE TABLE documents (
            id VARCHAR PRIMARY KEY,
            content TEXT,
            content_tokens TEXT
        )
    `)
    if err != nil {
        return err
    }
    
    // 创建支持 Sego 分词的 FTS 索引
    // 如果 content_tokens 列不存在，会自动创建
    err = duckdb_driver.CreateFTSIndexWithSego(
        ctx, db,
        "documents",    // 表名
        "id",           // ID 列
        "content",      // 内容列
        "content_tokens", // 分词列（可选，默认是 content + "_tokens"）
    )
    return err
}
```

#### 3. 插入文档并自动分词

```go
func insertDocument(ctx context.Context, db *sql.DB, id, content string) error {
    // 使用 Sego 分词
    tokens := duckdb_driver.TokenizeWithSego(content)
    
    // 插入文档
    _, err := db.ExecContext(ctx, `
        INSERT INTO documents (id, content, content_tokens)
        VALUES (?, ?, ?)
    `, id, content, tokens)
    return err
}
```

#### 4. 使用分词进行搜索

```go
func searchDocuments(ctx context.Context, db *sql.DB, query string) ([]string, error) {
    // 使用 Sego 分词搜索
    ids, err := duckdb_driver.SearchWithSego(
        ctx, db,
        "documents",    // 表名
        query,          // 搜索查询
        "content",      // 内容列
        "content_tokens", // 分词列（可选）
        10,             // 返回结果数量限制
    )
    return ids, err
}
```

#### 5. 更新文档的分词结果

```go
func updateDocumentTokens(ctx context.Context, db *sql.DB, docID string) error {
    // 自动从 content 列读取内容，分词后更新 content_tokens
    err := duckdb_driver.UpdateContentTokens(
        ctx, db,
        "documents",    // 表名
        "id",           // ID 列
        docID,          // 文档 ID
        "content",      // 内容列
        "content_tokens", // 分词列（可选）
    )
    return err
}
```

## API 文档

### TokenizeWithSego

对文本进行中文分词。

```go
func TokenizeWithSego(text string) string
```

**参数：**
- `text`: 要分词的文本

**返回：**
- 用空格分隔的分词结果

### CreateFTSIndexWithSego

创建支持 Sego 分词的 FTS 索引。

```go
func CreateFTSIndexWithSego(
    ctx context.Context,
    db *sql.DB,
    tableName, idColumn, contentColumn, tokensColumn string,
) error
```

**参数：**
- `ctx`: 上下文
- `db`: DuckDB 数据库连接
- `tableName`: 表名
- `idColumn`: ID 列名
- `contentColumn`: 内容列名
- `tokensColumn`: 分词结果列名（可选，默认为 `contentColumn + "_tokens"`）

### UpdateContentTokens

更新指定文档的分词结果。

```go
func UpdateContentTokens(
    ctx context.Context,
    db *sql.DB,
    tableName, idColumn, idValue, contentColumn, tokensColumn string,
) error
```

### SearchWithSego

使用 Sego 分词进行全文搜索。

```go
func SearchWithSego(
    ctx context.Context,
    db *sql.DB,
    tableName, query, contentColumn, tokensColumn string,
    limit int,
) ([]string, error)
```

**返回：**
- 匹配的文档 ID 列表

## 完整示例

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "log"
    
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
    duckdb_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
)

func main() {
    ctx := context.Background()
    
    // 打开数据库
    db, err := sql.Open("duckdb", "example.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
    
    // 创建表
    _, err = db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS documents (
            id VARCHAR PRIMARY KEY,
            content TEXT,
            content_tokens TEXT
        )
    `)
    if err != nil {
        log.Fatal(err)
    }
    
    // 创建 FTS 索引
    err = duckdb_driver.CreateFTSIndexWithSego(ctx, db, "documents", "id", "content", "content_tokens")
    if err != nil {
        log.Fatal(err)
    }
    
    // 插入文档
    content := "这是一个测试文档，用于演示中文分词功能"
    tokens := duckdb_driver.TokenizeWithSego(content)
    _, err = db.ExecContext(ctx, `
        INSERT INTO documents (id, content, content_tokens)
        VALUES (?, ?, ?)
    `, "doc1", content, tokens)
    if err != nil {
        log.Fatal(err)
    }
    
    // 搜索
    ids, err := duckdb_driver.SearchWithSego(ctx, db, "documents", "测试", "content", "content_tokens", 10)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println("找到的文档:", ids)
}
```

## 注意事项

1. **自动列创建**：`CreateFTSIndexWithSego` 会自动检查并创建 `tokensColumn` 列（如果不存在）
2. **向后兼容**：如果 `tokensColumn` 不存在，搜索功能会自动回退到使用原始 `contentColumn`
3. **分词失败处理**：如果 Sego 分词器初始化失败，`TokenizeWithSego` 会返回原文
4. **FTS 索引**：DuckDB 的 FTS 索引会自动维护，无需手动更新

## 依赖

- `github.com/marcboeker/go-duckdb/v2`: DuckDB Go 驱动
- `github.com/mozhou-tech/sqlite-ai-driver/pkg/sego`: Sego 中文分词器

