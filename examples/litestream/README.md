# Litestream 示例

本示例演示如何使用 Litestream 进行 SQLite 数据库的备份和恢复。

## 关于 Litestream

Litestream 是一个用于 SQLite 数据库的流式复制工具，能够将数据库的增量更改实时同步到本地文件或远程存储（如 S3、GCS 等）。

## 前置要求

1. **安装 Litestream Go 库依赖**：
   ```bash
   go get github.com/benbjohnson/litestream@latest
   go mod tidy
   ```

2. **确保所有依赖已安装**：
   ```bash
   go mod tidy
   ```

## 功能特性

- ✅ 使用 `modernc.org/sqlite` 驱动（纯 Go 实现）
- ✅ 自动启用 WAL（Write-Ahead Logging）模式
- ✅ 演示基本的数据库操作（CRUD）
- ✅ 使用 Litestream Go 库进行实时备份
- ✅ 支持本地文件系统备份
- ✅ 演示快照创建和同步功能

## 运行示例

```bash
cd examples/litestream
go run main.go
```

## 代码说明

### 数据库连接

示例使用 `modernc.org/sqlite` 驱动，并自动配置 WAL 模式：

```go
dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
db, err := sql.Open("sqlite", dsn)
```

**重要提示**：
- Litestream 需要 WAL 模式才能正常工作
- `busy_timeout` 设置为 5000 毫秒，确保复制过程的稳定性

### 基本操作

示例包含以下数据库操作：
- 创建表
- 插入数据
- 查询数据
- 统计数量

### Litestream 集成

示例使用 Litestream Go 库直接集成到应用程序中：

```go
import (
    "github.com/benbjohnson/litestream"
    "github.com/benbjohnson/litestream/file"
)

// 创建 Litestream 数据库实例
lsDB := litestream.NewDB(dbPath)
lsDB.MonitorInterval = 1 * time.Second
lsDB.CheckpointInterval = 1 * time.Minute

// 创建文件副本客户端
fileClient := file.NewReplicaClient()
fileClient.Path = backupDir

// 创建副本并附加到数据库
replica := litestream.NewReplica(lsDB)
replica.Client = fileClient
replica.SyncInterval = 1 * time.Second
lsDB.Replica = replica

// 打开数据库并开始复制
if err := lsDB.Open(); err != nil {
    log.Fatal(err)
}
defer lsDB.Close(ctx)

// 手动同步
if err := lsDB.Sync(ctx); err != nil {
    log.Fatal(err)
}

// 创建快照
info, err := lsDB.Snapshot(ctx)
```

## 实际使用建议

### 1. 使用 S3 备份

如果需要备份到 S3，可以使用 S3 客户端：

```go
import "github.com/benbjohnson/litestream/s3"

// 创建 S3 副本客户端
s3Client := s3.NewReplicaClient()
s3Client.AccessKeyID = "your-access-key-id"
s3Client.SecretAccessKey = "your-secret-access-key"
s3Client.Region = "us-east-1"
s3Client.Bucket = "your-bucket-name"
s3Client.Path = "app"

// 创建副本
replica := litestream.NewReplica(lsDB)
replica.Client = s3Client
lsDB.Replica = replica
```

### 2. 恢复数据库

使用 Litestream Go 库恢复数据库：

```go
import "github.com/benbjohnson/litestream/s3"

// 创建副本用于恢复
s3Client := s3.NewReplicaClient()
s3Client.Region = "us-east-1"
s3Client.Bucket = "your-bucket-name"
s3Client.Path = "app"

replica := litestream.NewReplicaWithClient(nil, s3Client)

// 恢复选项
opts := litestream.NewRestoreOptions()
opts.OutputPath = "/path/to/restored.db"
opts.Parallelism = 8

// 执行恢复
if err := replica.Restore(ctx, opts); err != nil {
    log.Fatal(err)
}
```

### 3. 配置监控和检查点

可以根据需要调整监控和检查点间隔：

```go
lsDB.MonitorInterval = 1 * time.Second      // 监控间隔
lsDB.CheckpointInterval = 1 * time.Minute    // 检查点间隔
lsDB.MinCheckpointPageN = 1000               // 最小检查点页数
lsDB.MaxCheckpointPageN = 10000             // 最大检查点页数
replica.SyncInterval = 1 * time.Second       // 同步间隔
```

## 注意事项

1. **WAL 模式要求**：Litestream 需要 SQLite 数据库运行在 WAL 模式下
2. **文件锁定**：确保应用程序和 Litestream 进程不会同时锁定数据库文件
3. **性能影响**：Litestream 复制过程对数据库性能影响很小，但建议在生产环境中监控
4. **备份策略**：建议配置定期快照和保留策略

## 更多资源

- [Litestream 官方文档](https://litestream.io/)
- [Litestream Go 库指南](https://litestream.io/guides/go-library/)
- [Litestream GitHub 仓库](https://github.com/benbjohnson/litestream)
- [SQLite WAL 模式文档](https://www.sqlite.org/wal.html)

## 依赖说明

本示例需要以下依赖：

- `github.com/benbjohnson/litestream` - Litestream 核心库
- `github.com/benbjohnson/litestream/file` - 文件系统副本客户端
- `modernc.org/sqlite` - SQLite 驱动（纯 Go 实现）

安装命令：
```bash
go get github.com/benbjohnson/litestream@latest
go mod tidy
```

## 相关规则

本示例遵循项目的 Litestream 备份恢复规则：
- 使用 `modernc.org/sqlite` 驱动（非 go-sqlite3）
- 默认关闭，需要显式配置开启
- 提供数据库备份和恢复功能

