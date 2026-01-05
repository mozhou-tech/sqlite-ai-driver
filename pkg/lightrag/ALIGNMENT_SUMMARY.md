# LightRAG Go版本与Python版本对齐总结

## 对齐完成情况

### 1. 查询模式对齐 ✅

已添加 `ModeMix` 模式，与 Python 版本保持一致：

- **ModeLocal**: 使用 low-level keywords，检索包含实体及其一度邻居的文档
- **ModeGlobal**: 使用 high-level keywords，检索更广泛相关的文档
- **ModeHybrid**: 结合 Local 和 Global 的结果
- **ModeMix**: 整合知识图谱和向量检索（新增）
- **ModeGraph**: 纯知识图谱查询，深度为 2
- **ModeVector/Naive**: 向量搜索
- **ModeFulltext**: 全文搜索

### 2. Prompt 模板对齐 ✅

Go 版本的 prompt 模板已与 Python 版本对齐：

- **实体提取 Prompt**: 提取实体和关系，输出 JSON 格式
- **查询关键词提取 Prompt**: 提取 low-level 和 high-level 关键词
- **RAG 答案生成 Prompt**: 基于上下文和问题生成答案

### 3. 检索算法对齐 ✅

检索算法已与 Python 版本对齐：

- **Local 模式**: 使用 `retrieveByKeywords` 方法，传入 low-level keywords
- **Global 模式**: 使用 `retrieveByKeywords` 方法，传入 high-level keywords
- **Hybrid 模式**: 分别执行 Local 和 Global，然后合并结果
- **Mix 模式**: 使用所有关键词（low-level + high-level）调用 `retrieveByKeywords`，该方法已实现图谱 + 向量的组合检索

### 4. 图查询模型保留 ✅

Go 版本独特的 GraphQuery 接口已完整保留：

- `V(node)`: 选择指定节点
- `Out(predicate)`: 获取出向邻居
- `In(predicate)`: 获取入向邻居
- `Both()`: 获取双向邻居
- `All(ctx)`: 执行查询并返回所有结果

该接口提供了灵活的图查询能力，与 Python 版本的功能对等。

### 5. 实体提取逻辑对齐 ✅

实体提取逻辑已与 Python 版本一致：

- 使用 LLM 提取实体和关系
- 存储实体到文档的链接（`APPEARS_IN`）
- 存储实体的类型（`TYPE`）和描述（`DESCRIPTION`）
- 存储实体间的关系

### 6. 测试覆盖 ✅

已添加 ModeMix 的测试用例，验证：
- Mix 模式的基本功能
- 图谱和向量检索的结合
- 三元组的召回

## 主要变更

1. **添加 ModeMix 模式** (`pkg/lightrag/types.go`)
   - 新增 `ModeMix` 常量
   - 在 `Retrieve` 方法中实现 Mix 模式逻辑

2. **完善测试** (`pkg/lightrag/lightrag_test.go`, `pkg/lightrag/modes_eval_test.go`)
   - 添加 ModeMix 的测试用例
   - 验证 Mix 模式的功能

## 保留的 Go 特性

1. **GraphQuery 接口**: Go 版本独特的图查询构建器接口，提供了灵活的图查询能力
2. **并发处理**: 使用 `errgroup` 实现并发检索
3. **错误处理**: Go 风格的错误处理机制

## 与 Python 版本的差异

1. **语言特性**: Go 版本充分利用了 Go 语言的并发特性
2. **存储后端**: Go 版本使用 DuckDB + Cayley，Python 版本支持更多存储后端
3. **API 风格**: Go 版本使用同步 API（虽然内部有异步处理），Python 版本使用异步 API

## 验证方法

运行测试以验证对齐：

```bash
cd pkg/lightrag
go test -v -run TestLightRAG_Retrieve_Modes_Extra
go test -v -run TestEvaluateModes
go test -v -run TestEvaluateAdvancedModes
```

## 后续建议

1. 考虑添加更多存储后端支持（如 PostgreSQL、Redis 等）
2. 完善文档和示例代码
3. 性能优化和基准测试
4. 考虑添加 Reranker 支持（Python 版本已支持）

