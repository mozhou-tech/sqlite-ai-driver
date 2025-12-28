# LightRAG Chatbot

这是一个基于 eino-lightrag 的多轮对话示例应用。

## 功能特性

1. **后端基于 Gin 和 eino 实现**，使用本仓库提供的 eino-ext 包提供的 lightrag 扩展
2. **前端基于最新版 Next.js**，使用 shadcn/ui 组件库
3. **大模型基于环境变量配置的 OpenAI 模型**
4. **提供简单的多轮对话示例**，用于品策 eino-lightrag 的效果

## 项目结构

```
chatbot/
├── backend/          # Go 后端服务
│   ├── main.go      # 主程序入口
│   └── go.mod       # Go 依赖管理
├── frontend/         # Next.js 前端应用
│   ├── app/         # Next.js App Router
│   ├── components/  # React 组件
│   └── lib/         # 工具函数
└── README.md        # 本文件
```

## 环境变量配置

### 后端环境变量

在运行后端服务前，需要设置以下环境变量：

```bash
# OpenAI API 配置（必需）
export OPENAI_API_KEY="your-openai-api-key"

# OpenAI 模型（可选，默认为 gpt-4o-mini）
export OPENAI_MODEL="gpt-4o-mini"

# OpenAI Base URL（可选，默认为 https://api.openai.com/v1）
export OPENAI_BASE_URL="https://api.openai.com/v1"

# RAG 工作目录（可选，默认为 ./rag_storage）
export RAG_WORKING_DIR="./rag_storage"

# 服务端口（可选，默认为 8080）
export PORT="8080"
```

### 前端环境变量

创建 `frontend/.env.local` 文件：

```bash
# 后端 API 地址（可选，默认为 http://localhost:8080/api）
NEXT_PUBLIC_API_URL=http://localhost:8080/api
```

## 快速开始

### 1. 启动后端服务

```bash
cd chatbot/backend
go mod tidy
go run main.go
```

后端服务将在 `http://localhost:8080` 启动。

### 2. 启动前端应用

```bash
cd chatbot/frontend
npm install
npm run dev
```

前端应用将在 `http://localhost:3000` 启动。

### 3. 添加文档

在开始对话前，您可以通过 API 添加文档到知识库：

```bash
curl -X POST http://localhost:8080/api/documents \
  -H "Content-Type: application/json" \
  -d '{"content": "您的文档内容..."}'
```

### 4. 开始对话

打开浏览器访问 `http://localhost:3000`，开始与 chatbot 对话。

## API 接口

### POST /api/chat

发送聊天消息并获取回复。

**请求体：**
```json
{
  "message": "您的问题",
  "history": ["历史消息1", "历史消息2"]
}
```

**响应：**
```json
{
  "response": "AI 回复内容"
}
```

### POST /api/documents

添加文档到知识库。

**请求体：**
```json
{
  "content": "文档内容"
}
```

**响应：**
```json
{
  "id": "success"
}
```

### GET /api/documents

获取文档列表（当前为简单实现）。

## 技术栈

### 后端
- **Gin**: Web 框架
- **eino**: LLM 应用框架
- **lightrag**: LightRAG 实现
- **eino-ext**: eino 扩展包

### 前端
- **Next.js 15**: React 框架
- **TypeScript**: 类型安全
- **Tailwind CSS**: 样式框架
- **shadcn/ui**: UI 组件库
- **Radix UI**: 无障碍组件

## 开发说明

### 后端开发

后端使用 Go 1.24.2 开发，主要依赖：
- `github.com/gin-gonic/gin`: Web 框架
- `github.com/cloudwego/eino`: LLM 应用框架
- `github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag`: LightRAG 实现

### 前端开发

前端使用 Next.js 15 和 TypeScript 开发，主要特性：
- App Router 架构
- 服务端组件和客户端组件
- Tailwind CSS 样式
- shadcn/ui 组件系统

## 注意事项

1. 确保已正确配置 OpenAI API Key
2. 首次运行时会创建 RAG 存储目录
3. 文档索引和实体提取在后台异步执行
4. 多轮对话历史目前仅用于上下文，未实现完整的对话状态管理

## 许可证

本项目遵循 Apache License 2.0 许可证。
