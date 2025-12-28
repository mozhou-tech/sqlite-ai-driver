# LightRAG 规则与指南

LightRAG 是一个受 [LightRAG 论文](https://arxiv.org/abs/2410.05779) 启发的图增强检索增强生成（Graph-enhanced RAG）框架。它通过构建知识图谱来捕捉实体间的关系，从而提供比传统 RAG 更精准、更具上下文相关性的检索结果。

## 核心概念

- **双层检索 (Dual-Level Retrieval)**: 结合了局部（基于实体的近邻）和全局（基于更广泛的关系和社区）检索。
- **图索引**: 从文本分块中提取实体（Entities）和关系（Relationships），并将其存储在图数据库中。
- **检索模式 (`QueryMode`) 详述**:
  - `ModeNaive` / `ModeVector`: 传统 RAG 模式。仅使用向量搜索召回相关文档，不涉及任何知识图谱信息。适用于通用事实查询。
  - `ModeFulltext`: 纯文本匹配模式。利用全文搜索（如 BM25）召回文档。适用于关键词匹配强相关的查询。
  - `ModeLocal`: **局部图检索**。提取查询中的实体，并召回包含这些实体及其“一度邻居”实体的文档。它主要关注与实体直接相关的具体事实。
  - `ModeGlobal`: **全局图检索**。通过图谱中的关系扩展检索范围。在本项目实现中，它会召回相关三元组，并结合混合搜索的结果，旨在提供更宏观的上下文。
  - `ModeHybrid`: **混合检索（推荐）**。同时运行向量搜索、全文搜索和图谱检索，并使用 **RRF (Reciprocal Rank Fusion)** 算法对结果进行融合排序。这是最鲁棒的模式，兼顾了语义、关键词和关联性。
  - `ModeGraph`: **纯图谱检索**。不进行常规的文档搜索，而是深度探索（深度为 2）与查询实体相关的子图，并根据子图关联的实体来反向召回文档。适用于需要复杂关系推理的场景。

## 代码实现规范 (Go)

### 1. 初始化
- 使用 `lightrag.New(opts)` 创建实例。
- 必须调用 `InitializeStorages(ctx)` 来初始化底层存储（如 SQLite/DuckDB 和 Cayley）。
- 确保提供了 `Embedder` 和 `LLM` 实现。

### 2. 数据处理
- `Insert(ctx, text)`: 将文档存入集合，并**异步**启动 LLM 提取实体和关系。
- `InsertBatch`: 批量处理文档。注意：由于图提取涉及 LLM，内部使用信号量 (`llmSem`) 控制并发。

### 3. 检索与查询
- 优先使用 `Query(ctx, query, param)` 获取最终答案。
- 如果只需要召回内容，使用 `Retrieve(ctx, query, param)`。
- `QueryParam` 中的 `Mode` 决定了召回算法。

### 4. 存储架构
- **文档存储**: 存储在 `documents` 集合中。
- **向量/全文**: 通过 `AddVectorSearch` 和 `AddFulltextSearch` 为文档集合添加索引。
- **图数据库**: 使用 Cayley 后端。
  - 谓词 `APPEARS_IN`: 链接实体到其来源文档 ID。
  - 谓词 `TYPE`: 实体的类型。
  - 谓词 `DESCRIPTION`: 实体的描述。
  - 其他自定义谓词：表示实体间的领域关系。

### 5. 并发与资源管理
- 内部使用 `sync.WaitGroup` 跟踪后台提取任务。
- 调用 `FinalizeStorages(ctx)` 确保所有后台任务完成并关闭数据库连接。
- 通过 `Options.MaxConcurrentLLM` 限制 LLM 并发量，防止触发 API 限流。

## 开发建议
- **提示词定制**: 提取逻辑和查询逻辑依赖于 `prompts.go` 中的模板，可根据业务场景调整。
- **调试**: 查看日志中的 `Recalled: Entity ...` 和 `Graph Recalled: ...` 来验证图谱召回是否符合预期。
- **可视化**: 可以使用 `ExportGraph` 导出三元组，用于前端图谱展示。

