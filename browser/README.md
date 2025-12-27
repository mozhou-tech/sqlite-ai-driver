# RxDB-Go 数据浏览器

一个用于浏览和管理 RxDB-Go 数据库的 Web 应用程序。

## 技术栈

- **前端**: React + Vite + TypeScript + Tailwind CSS + shadcn/ui
- **后端**: Go + Gin

## 功能特性

- 📄 **文档浏览**: 查看、创建、更新、删除文档
- 🔍 **全文搜索**: 基于全文索引的文档搜索
- 🎯 **向量搜索**: 基于向量相似度的文档搜索

## 目录结构

```
browser/
├── api/                    # 后端 API 服务器
│   ├── main.go            # API 主程序
│   └── go.mod             # Go 模块文件
├── src/
│   ├── components/        # React 组件
│   │   ├── Layout.tsx     # 布局组件
│   │   └── ui/            # UI 组件库
│   ├── pages/             # 页面组件
│   │   ├── DocumentsPage.tsx      # 文档浏览页面
│   │   ├── FulltextSearchPage.tsx # 全文搜索页面
│   │   └── VectorSearchPage.tsx    # 向量搜索页面
│   ├── utils/             # 工具函数
│   │   ├── api.ts         # API 客户端
│   │   └── cn.ts          # 样式工具
│   ├── styles/            # 样式文件
│   │   └── index.css      # 全局样式
│   ├── App.tsx            # 应用主组件
│   └── main.tsx           # 入口文件
├── package.json           # 前端依赖配置
├── vite.config.ts         # Vite 配置
├── tailwind.config.js     # Tailwind CSS 配置
└── .env.example           # 环境变量示例
```

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

### 2. 启动开发服务器

```bash
pnpm dev
```

此命令会同时启动：
- **后端 API 服务器**: `http://localhost:40111`
- **前端开发服务器**: `http://localhost:40112`

如果需要单独启动，可以使用：
- `pnpm dev:api` - 仅启动后端
- `pnpm dev:frontend` - 仅启动前端

环境变量（可选）:
- `DB_NAME`: 数据库名称（默认: `browser-db`）
- `DB_PATH`: 数据库路径（默认: `./data/browser-db`）
- `PORT`: 服务器端口（默认: `40111`）
- `DASHSCOPE_API_KEY`: DashScope API 密钥（用于生成 embedding，向量搜索功能需要）

### 3. 生成示例数据（可选）

**注意**: 运行 seed 命令前，请确保 API 服务器已停止，否则会出现数据库锁定错误。

```bash
# 使用 Makefile
make seed

# 或直接运行
cd api
go run seed.go
```

这将创建两个集合：
- **articles**: 8 篇文章（已创建全文搜索索引，可直接搜索）
- **products**: 10 个产品（已创建向量搜索索引，可直接搜索）

生成的示例数据已包含全文搜索和向量搜索索引，可以直接在浏览器中使用。

### 4. 构建生产版本

```bash
# 构建前端
pnpm build

# 构建后端（可选）
cd api
go build -o browser-api main.go
```

## API 端点

### 文档操作

- `GET /api/collections/:name/documents` - 获取文档列表
- `GET /api/collections/:name/documents/:id` - 获取单个文档
- `POST /api/collections/:name/documents` - 创建文档
- `PUT /api/collections/:name/documents/:id` - 更新文档
- `DELETE /api/collections/:name/documents/:id` - 删除文档

### 全文搜索

- `POST /api/collections/:name/fulltext/search` - 执行全文搜索

请求体:
```json
{
  "collection": "articles",
  "query": "搜索关键词",
  "limit": 10,
  "threshold": 0.0
}
```

### 向量搜索

- `POST /api/collections/:name/vector/search` - 执行向量搜索

请求体（向量查询）:
```json
{
  "collection": "products",
  "query": [0.1, 0.2, 0.3, ...],
  "limit": 10,
  "field": "embedding"
}
```

请求体（文本查询，自动生成 embedding）:
```json
{
  "collection": "products",
  "query_text": "智能手机",
  "limit": 10,
  "field": "embedding"
}
```

**注意**: `query` 和 `query_text` 二选一。如果提供 `query_text`，系统会使用 DashScope API 自动生成 embedding。

## 使用说明

### 文档浏览

1. 在"文档浏览"页面输入集合名称（例如: `articles`）
2. 点击"刷新"按钮加载文档
3. 使用分页按钮浏览更多文档
4. 点击"删除"按钮删除文档

### 全文搜索

1. 在"全文搜索"页面输入集合名称和搜索关键词
2. 设置结果数量限制（可选）
3. 点击"搜索"按钮执行搜索
4. 查看搜索结果和相关性分数

### 向量搜索

支持两种查询方式：

**方式一：文本查询（推荐）**
1. 在"向量搜索"页面选择"文本查询"模式
2. 输入集合名称（例如: `products`）
3. 输入查询文本（例如: `智能手机`、`运动鞋` 等）
4. 系统会自动使用 DashScope 生成 embedding 并搜索
5. 查看搜索结果和相似度分数

**方式二：向量查询**
1. 在"向量搜索"页面选择"向量查询"模式
2. 输入集合名称和向量字段名
3. 输入查询向量（JSON 数组或逗号分隔的数字）
   - 示例: `[0.1, 0.2, 0.3]` 或 `0.1, 0.2, 0.3`
4. 设置结果数量限制（可选）
5. 点击"搜索"按钮执行搜索
6. 查看搜索结果和相似度分数

## 注意事项

1. **全文搜索**: 需要先为集合创建全文搜索索引。API 会在首次搜索时自动创建索引。
2. **向量搜索**: 文档必须包含向量字段（默认字段名为 `embedding`）。向量维度会在首次搜索时自动推断。
3. **集合 Schema**: 当前实现使用默认 schema。如需自定义 schema，需要修改后端代码。

## 开发

### 前端开发

```bash
pnpm dev           # 同时启动前端和后端开发服务器
pnpm dev:api       # 仅启动后端 API 服务器
pnpm dev:frontend  # 仅启动前端开发服务器
pnpm build         # 构建生产版本
pnpm preview       # 预览生产构建
pnpm lint          # 运行 ESLint
```

### 后端开发

```bash
cd api
go run main.go   # 运行开发服务器
go build         # 构建可执行文件
go test          # 运行测试
```

## 许可证

MIT
