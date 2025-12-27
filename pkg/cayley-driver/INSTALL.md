# 安装说明

## 依赖安装

本模块使用 `github.com/mattn/go-sqlite3` 作为 SQLite3 驱动。

**注意**：此驱动需要 CGO 支持，需要系统安装 SQLite3 开发库。

### 系统依赖

#### macOS
```bash
brew install sqlite3
```

#### Linux (Ubuntu/Debian)
```bash
sudo apt-get install libsqlite3-dev
```

#### Linux (CentOS/RHEL)
```bash
sudo yum install sqlite-devel
```

### 安装步骤

1. 在项目根目录运行：

```bash
go get github.com/mattn/go-sqlite3
go mod tidy
```

2. 或者手动添加到 `go.mod`：

```go
require (
    github.com/mattn/go-sqlite3 v1.14.32
)
```

然后运行 `go mod tidy`。

### CGO 要求

确保 CGO 已启用（默认情况下是启用的）。如果需要显式启用，可以设置环境变量：

```bash
export CGO_ENABLED=1
```

## 验证安装

运行测试验证安装是否成功：

```bash
cd pkg/cayley-driver
go test -v
```

## 运行示例

```bash
cd examples/cayley-graph
go run main.go
```

