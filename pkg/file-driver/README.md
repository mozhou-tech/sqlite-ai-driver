# File Driver 使用指南

File Driver 是一个基于 `github.com/viant/afs` 的 SQLite 数据库驱动，支持从多种存储后端读取 SQLite 数据库文件。

## 功能特性

- ✅ 支持本地文件系统
- ✅ 支持 AWS S3
- ✅ 支持 Google Cloud Storage (GCS)
- ✅ 支持其他 afs 支持的存储后端
- ✅ 自动处理远程文件的下载和清理

## 安装

确保依赖已正确安装：

```bash
go mod tidy
```

## 基本使用

### 1. 导入驱动

```go
import (
    "database/sql"
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/file-driver"
)
```

### 2. 打开数据库连接

#### 本地文件系统

```go
// 方式 1: 直接使用文件路径（推荐）
db, err := sql.Open("file", "/path/to/database.db")

// 方式 2: 使用 file:// 协议
db, err := sql.Open("file", "file:///path/to/database.db")
```

#### AWS S3

```go
// 需要配置 AWS 凭证
// 方式 1: 环境变量
// export AWS_ACCESS_KEY_ID=your_access_key
// export AWS_SECRET_ACCESS_KEY=your_secret_key
// export AWS_REGION=us-east-1

// 方式 2: ~/.aws/credentials 文件

db, err := sql.Open("file", "s3://bucket-name/path/to/database.db")
```

#### Google Cloud Storage

```go
// 需要配置 GCS 凭证
// export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json

db, err := sql.Open("file", "gs://bucket-name/path/to/database.db")
```

### 3. 完整示例

```go
package main

import (
    "database/sql"
    "log"
    
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/file-driver"
)

func main() {
    // 打开数据库
    db, err := sql.Open("file", "test.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // 测试连接
    if err := db.Ping(); err != nil {
        log.Fatal(err)
    }

    // 执行查询
    rows, err := db.Query("SELECT * FROM users")
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()

    // 处理结果...
}
```

## 工作原理

1. **本地文件**: 直接使用 sqlite3 驱动打开，无需额外处理
2. **远程文件**: 
   - 自动下载到临时文件
   - 使用 sqlite3 驱动打开临时文件
   - 连接关闭时自动清理临时文件

## 注意事项

### 远程文件的写入限制

⚠️ **重要**: 对于远程存储（S3、GCS 等），文件会被下载到本地临时文件进行操作。这意味着：

- ✅ 可以正常读取数据
- ✅ 可以执行写入操作（写入到临时文件）
- ❌ **写入不会自动同步回远程存储**
- ❌ 如果需要在远程存储中持久化更改，需要手动上传文件

### 凭证配置

#### AWS S3

```bash
# 方式 1: 环境变量
export AWS_ACCESS_KEY_ID=your_access_key
export AWS_SECRET_ACCESS_KEY=your_secret_key
export AWS_REGION=us-east-1

# 方式 2: AWS CLI 配置
aws configure
```

#### Google Cloud Storage

```bash
# 设置服务账号密钥文件路径
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json

# 或者使用 gcloud 认证
gcloud auth application-default login
```

## 支持的存储后端

File Driver 基于 `github.com/viant/afs`，支持以下存储后端：

- 本地文件系统 (`file://`)
- AWS S3 (`s3://`)
- Google Cloud Storage (`gs://`)
- 其他 afs 支持的存储后端

更多信息请参考 [afs 文档](https://pkg.go.dev/github.com/viant/afs)。

## 示例代码

完整示例请参考 `examples/file-driver-example.go`。

