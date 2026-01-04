# GraphSearch 目录重组说明

## 已完成的工作

1. ✅ 创建了模块目录结构：
   - `core/` - 核心功能
   - `config/` - 配置管理
   - `graphrag/` - GraphRAG 组件
   - `indexer/` - 索引功能
   - `optimization/` - 优化功能
   - `community/` - 社区发现
   - `llm/` - LLM 接口
   - `prompts/` - 提示词管理
   - `utils/` - 工具函数

2. ✅ 已移动核心文件到 `core/` 目录：
   - `graphsearch.go` - 主入口
   - `types.go` - 类型定义
   - `entity.go` - 实体操作
   - `graph.go` - 图操作
   - `search.go` - 搜索功能

## 待完成的工作

### 1. 移动剩余文件到对应目录

需要将以下文件移动到对应目录：

**config/**
- config.go
- config_test.go

**graphrag/**
- query_processor.go
- query_processor_test.go
- query_intent.go
- query_intent_test.go
- retriever.go
- retriever_test.go
- organizer.go
- organizer_test.go
- generator.go
- generator_test.go
- graphrag_test.go
- example_test.go

**indexer/**
- indexer.go
- indexer_test.go

**optimization/**
- graph_optimization.go
- graph_optimization_test.go
- vector_optimization.go
- vector_optimization_test.go

**community/**
- community.go
- community_test.go
- community_organization.go
- community_organization_test.go

**llm/**
- llm.go
- llm_test.go

**prompts/**
- prompts.go
- prompts_test.go

**utils/**
- utils.go
- utils_test.go

### 2. 更新包声明

由于 Go 的包系统限制，文件在不同目录必须是不同的包。需要：
- 将每个子目录的文件包名改为对应的包名（如 `package core`, `package graphrag` 等）
- 在根目录创建 `graphsearch.go` 文件，保持 `package graphsearch`，重新导出所有公共 API

### 3. 更新导入路径

更新所有文件中的导入路径，使其指向正确的子包。

### 4. 删除旧文件

删除根目录下已移动的文件。

## 注意事项

- 所有文件需要保持 `package graphsearch` 的公共 API 不变
- 测试文件需要跟随对应的源文件一起移动
- `test_helpers_test.go` 可以保留在根目录或移动到合适的位置

