# 参考资料：
 1. LightRag论文：https://arxiv.org/abs/2511.06650
 2. 基于DuckDB和Cayley实现的LightRag
 3. Git仓库：https://github.com/HKUDS/LightRAG

# 模块介绍
 1. 基于DuckDB和Cayley实现的LightRag
 2. Python版本API
    ```python
    import os
    import asyncio
    from lightrag import LightRAG, QueryParam
    from lightrag.llm.openai import gpt_4o_mini_complete, gpt_4o_complete, openai_embed
    from lightrag.utils import setup_logger

    setup_logger("lightrag", level="INFO")

    WORKING_DIR = "./rag_storage"
    if not os.path.exists(WORKING_DIR):
        os.mkdir(WORKING_DIR)

    async def initialize_rag():
        rag = LightRAG(
            working_dir=WORKING_DIR,
            embedding_func=openai_embed,
            llm_model_func=gpt_4o_mini_complete,
        )
        # IMPORTANT: Both initialization calls are required!
        await rag.initialize_storages()  # Initialize storage backends    return rag

    async def main():
        try:
            # Initialize RAG instance
            rag = await initialize_rag()
            await rag.ainsert("Your text")

            # Perform hybrid search
            mode = "hybrid"
            print(
            await rag.aquery(
                "What are the top themes in this story?",
                param=QueryParam(mode=mode)
            )
            )

        except Exception as e:
            print(f"An error occurred: {e}")
        finally:
            if rag:
                await rag.finalize_storages()

    if __name__ == "__main__":
        asyncio.run(main())
    ```

# 待办事项

## 核心功能实现

### 1. LightRAG 核心模块 ⏳
- [ ] 实现 LightRAG 核心结构体和方法
- [ ] 设计 Go 版本的 API 接口，参考 Python 版本
- [x] 集成 DuckDB 和 Cayley 作为底层存储

### 2. 存储初始化 ⏳
- [ ] 实现 `InitializeStorages()` 方法
- [x] 初始化底层存储后端（DuckDB 数据库和集合，Cayley 图数据库）
- [ ] 配置向量存储、全文搜索等组件

### 3. 文本插入功能 ⏳
- [ ] 实现 `Insert()` 方法
- [ ] 支持文本向量化（集成 embedding 功能）
- [ ] 支持文本分块和索引构建

### 4. 查询功能 ⏳
- [ ] 实现 `Query()` 方法
- [ ] 支持多种查询模式：
  - `hybrid` - 混合搜索（向量 + 全文）
  - `vector` - 向量搜索
  - `fulltext` - 全文搜索
  - `graph` - 图搜索（可选）
- [ ] 实现 `QueryParam` 结构体用于配置查询参数

### 5. 资源清理 ⏳
- [ ] 实现 `FinalizeStorages()` 方法
- [ ] 清理和关闭存储资源
- [ ] 确保资源正确释放

## 集成功能

### 6. Embedding 集成 ⏳
- [ ] 集成 embedding 生成器（支持 OpenAI、DashScope、HuggingFace 等）
- [ ] 实现向量化文本处理流程

### 7. LLM 集成 ⏳
- [ ] 集成 LLM 模型（支持 OpenAI、DashScope 等）
- [ ] 实现查询结果的后处理和生成

### 8. 示例代码 ⏳
- [ ] 创建基本使用示例
- [ ] 创建完整功能演示示例
- [ ] 添加错误处理示例

## 文档完善

### 9. API 文档 ⏳
- [ ] 完善 API 文档说明
- [ ] 添加使用示例和最佳实践
- [ ] 添加配置选项说明

### 10. 测试 ⏳
- [ ] 编写单元测试
- [ ] 编写集成测试
- [ ] 添加性能测试
