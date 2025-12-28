实现对eino-ext全覆盖的测试用例

已完成的测试用例：
1. **TFIDFSplitter 单元测试**: 覆盖了相似度切分、Markdown 标题切分、空格处理等。
2. **DuckDB Indexer/Retriever 单元测试**: 覆盖了基本的存储和检索逻辑（带 Mock）。
3. **LightRAG Indexer/Retriever 单元测试**: 覆盖了 LightRAG 的集成存储和检索。
4. **全流程集成测试 (integration_test.go)**: 
   - 覆盖了从文档切分、索引到检索的完整 RAG 流程。
   - 验证了 DuckDB 向量存储与检索的正确性。
   - 验证了 LightRAG 混合检索（向量 + 全文）的正确性。
   - 验证了元数据 (Metadata) 和稠密向量 (Dense Vector) 的正确保存与返回。

修复的问题：
- 修复了 DuckDB 在存储和检索 `FLOAT[]` 向量时，参数类型不匹配的问题（统一转换为字符串表示）。
- 修复了 DuckDB 检索时 `metadata` JSON 列扫描失败的问题。
- 增强了 LightRAG 的全文检索实现，支持多词匹配。
