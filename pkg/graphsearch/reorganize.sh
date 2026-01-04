#!/bin/bash
set -e

# 创建目录
mkdir -p core config graphrag indexer optimization community llm prompts utils

# 移动核心文件
[ -f graphsearch.go ] && mv graphsearch.go core/ || true
[ -f types.go ] && mv types.go core/ || true
[ -f entity.go ] && mv entity.go core/ || true
[ -f graph.go ] && mv graph.go core/ || true
[ -f search.go ] && mv search.go core/ || true

# 移动配置
[ -f config.go ] && mv config.go config/ || true
[ -f config_test.go ] && mv config_test.go config/ || true

# 移动 GraphRAG
[ -f query_processor.go ] && mv query_processor.go graphrag/ || true
[ -f query_processor_test.go ] && mv query_processor_test.go graphrag/ || true
[ -f query_intent.go ] && mv query_intent.go graphrag/ || true
[ -f query_intent_test.go ] && mv query_intent_test.go graphrag/ || true
[ -f retriever.go ] && mv retriever.go graphrag/ || true
[ -f retriever_test.go ] && mv retriever_test.go graphrag/ || true
[ -f organizer.go ] && mv organizer.go graphrag/ || true
[ -f organizer_test.go ] && mv organizer_test.go graphrag/ || true
[ -f generator.go ] && mv generator.go graphrag/ || true
[ -f generator_test.go ] && mv generator_test.go graphrag/ || true
[ -f graphrag_test.go ] && mv graphrag_test.go graphrag/ || true
[ -f example_test.go ] && mv example_test.go graphrag/ || true

# 移动索引
[ -f indexer.go ] && mv indexer.go indexer/ || true
[ -f indexer_test.go ] && mv indexer_test.go indexer/ || true

# 移动优化
[ -f graph_optimization.go ] && mv graph_optimization.go optimization/ || true
[ -f graph_optimization_test.go ] && mv graph_optimization_test.go optimization/ || true
[ -f vector_optimization.go ] && mv vector_optimization.go optimization/ || true
[ -f vector_optimization_test.go ] && mv vector_optimization_test.go optimization/ || true

# 移动社区
[ -f community.go ] && mv community.go community/ || true
[ -f community_test.go ] && mv community_test.go community/ || true
[ -f community_organization.go ] && mv community_organization.go community/ || true
[ -f community_organization_test.go ] && mv community_organization_test.go community/ || true

# 移动 LLM
[ -f llm.go ] && mv llm.go llm/ || true
[ -f llm_test.go ] && mv llm_test.go llm/ || true

# 移动提示词
[ -f prompts.go ] && mv prompts.go prompts/ || true
[ -f prompts_test.go ] && mv prompts_test.go prompts/ || true

# 移动工具
[ -f utils.go ] && mv utils.go utils/ || true
[ -f utils_test.go ] && mv utils_test.go utils/ || true

echo "Files reorganized successfully!"

