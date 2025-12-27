# 快速启动指南

## 前置要求

- Node.js 18+ 和 pnpm
- Go 1.23+

## 快速开始

### 1. 安装依赖

```bash
# 安装前端依赖
pnpm install

# 安装后端依赖
cd api
go mod download
cd ..
```

### 2. 启动应用

#### 方式一：一键启动（推荐）

```bash
pnpm dev
```

此命令会同时启动前端和后端服务器。

#### 方式二：分别启动

如果需要单独启动，可以使用：

**仅启动后端：**
```bash
pnpm dev:api
# 或
cd api && go run main.go
```

**仅启动前端：**
```bash
pnpm dev:frontend
```

#### 方式三：使用 Makefile（如果已安装 make）

```bash
make dev
```

### 3. 生成示例数据（可选）

```bash
# 使用 Makefile
make seed

# 或直接运行
cd api
go run seed.go
```

这将创建：
- **articles** 集合：8 篇文章（用于测试全文搜索）
- **products** 集合：10 个产品（用于测试向量搜索）

### 4. 访问应用

- 前端: http://localhost:40112
- 后端 API: http://localhost:40111/api

## 测试数据

### 创建测试集合和文档

你可以使用以下方式创建测试数据：

1. **通过前端界面**：
   - 在"文档浏览"页面输入集合名称（如 `articles`）
   - 使用浏览器的开发者工具调用 API 创建文档

2. **通过 API**：
```bash
# 创建文档
curl -X POST http://localhost:40111/api/collections/articles/documents \
  -H "Content-Type: application/json" \
  -d '{
    "id": "article-1",
    "title": "Go 语言入门",
    "content": "Go 是一种开源的编程语言，由 Google 开发。"
  }'
```

### 测试全文搜索

1. 创建一些包含文本的文档
2. 在"全文搜索"页面输入集合名称和搜索关键词
3. 点击"搜索"查看结果

### 测试向量搜索

1. 创建包含向量字段的文档：
```bash
curl -X POST http://localhost:40111/api/collections/products/documents \
  -H "Content-Type: application/json" \
  -d '{
    "id": "prod-1",
    "name": "示例产品",
    "embedding": [0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8]
  }'
```

2. 在"向量搜索"页面输入查询向量（例如: `[0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8]`）
3. 点击"搜索"查看相似度结果

## 常见问题

## 端口被占用

如果 40111 或 40112 端口被占用，可以：

- 后端：设置环境变量 `PORT=40113`
- 前端：修改 `vite.config.ts` 中的 `server.port`

### 数据库路径

默认数据库存储在 `./data/browser-db`，可以通过环境变量 `DB_PATH` 修改。

### CORS 错误

后端已配置 CORS，允许所有来源。如需限制，修改 `api/main.go` 中的 CORS 配置。

