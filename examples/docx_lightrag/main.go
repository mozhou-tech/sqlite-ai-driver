package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	docxparser "github.com/cloudwego/eino-ext/components/document/parser/docx"
	openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/document/transformer/splitter/tfidf"
	lightrag "github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
)

func main() {
	ctx := context.Background()

	// 1. 设置环境变量和配置
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("请设置 OPENAI_API_KEY 环境变量")
		return
	}
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}

	// 初始化 sego 词典以获得更好的中文处理效果
	if err := sego.Init(); err != nil {
		log.Printf("警告：sego 初始化失败: %v", err)
	}

	// 2. 设置工作目录和 DOCX 文件路径
	workingDir := "./rag_storage"
	_ = os.MkdirAll(workingDir, 0755)

	docxPath := "/Users/mozhou/WorkSpace/supacloud/测试数据/淮河干支线数字化转型升级施工项目/招标文件与设计文件/淮河数字化连云港设计文件终稿.docx"

	// 检查文件是否存在
	if _, err := os.Stat(docxPath); os.IsNotExist(err) {
		log.Fatalf("DOCX 文件不存在: %s", docxPath)
	}

	// 3. 初始化 Embedder（使用 lightrag 的 OpenAI embedder）
	embedder, err := lightrag.NewOpenAIEmbedder(ctx, &openaiembedding.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   "text-embedding-v4",
	})
	if err != nil {
		log.Fatalf("创建 embedder 失败: %v", err)
	}

	// 4. 初始化 LLM（用于知识图谱提取）
	llm := lightrag.NewOpenAILLM(&lightrag.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})

	// 5. 创建 LightRAG 实例（完整版本，支持知识图谱）
	rag := lightrag.New(lightrag.Options{
		WorkingDir: workingDir,
		Embedder:   embedder,
		LLM:        llm,
	})

	// 初始化存储
	if err := rag.InitializeStorages(ctx); err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	defer rag.FinalizeStorages(ctx)

	// 6. 创建 TFIDF Splitter
	splitter, err := tfidf.NewTFIDFSplitter(ctx, &tfidf.Config{
		SimilarityThreshold:  0.2,
		MaxChunkSize:         800,  // 800 字符
		MinChunkSize:         700,  // 700 字符
		MaxSentencesPerChunk: 50,   // 最多 50 个句子
		UseSego:              true, // 使用 sego 进行中文分词
		IDGenerator: func(ctx context.Context, originalID string, splitIndex int) string {
			return fmt.Sprintf("%s_chunk_%d", originalID, splitIndex)
		},
		FilterGarbageChunks: true, // 启用乱码过滤
	})
	if err != nil {
		log.Fatalf("创建 TFIDF splitter 失败: %v", err)
	}

	// 7. 解析 DOCX 文件
	fmt.Printf("正在解析 DOCX 文件: %s\n", docxPath)
	docxParser, err := docxparser.NewDocxParser(ctx, &docxparser.Config{
		ToSections:      false, // 合并所有章节为一个文档
		IncludeComments: true,
		IncludeHeaders:  true,
		IncludeFooters:  true,
		IncludeTables:   true,
	})
	if err != nil {
		log.Fatalf("创建 DOCX 解析器失败: %v", err)
	}

	// 打开 DOCX 文件
	docxFile, err := os.Open(docxPath)
	if err != nil {
		log.Fatalf("打开 DOCX 文件失败: %v", err)
	}
	defer docxFile.Close()

	// 解析 DOCX
	var docs []*schema.Document
	docs, err = docxParser.Parse(ctx, docxFile)
	if err != nil {
		if len(docs) == 0 {
			log.Fatalf("解析 DOCX 完全失败: %v", err)
		}
		log.Printf("警告：DOCX 解析部分失败: %v，但已成功解析部分内容，将继续处理", err)
	}

	if len(docs) == 0 {
		log.Fatalf("解析 DOCX 后没有生成任何文档")
	}

	// 如果 ToSections 为 false，应该只有一个文档
	if len(docs) > 1 {
		log.Printf("警告：合并模式应该只生成一个文档，但生成了 %d 个文档", len(docs))
	}

	// 为文档设置 ID 和元数据
	baseID := filepath.Base(docxPath)
	doc := docs[0]
	if doc.ID == "" {
		doc.ID = baseID
	}
	if doc.MetaData == nil {
		doc.MetaData = make(map[string]any)
	}
	doc.MetaData["source_file"] = docxPath
	doc.MetaData["merged"] = true

	fmt.Printf("成功解析 DOCX，文档长度: %d 字符\n", len(doc.Content))

	// 将DOCX解析结果输出到Markdown文件
	mdOutputPath := filepath.Join(workingDir, "docx_parsed_output.md")
	fmt.Printf("正在将DOCX解析结果写入Markdown文件: %s\n", mdOutputPath)
	if err := writeDocxToMarkdown(mdOutputPath, docxPath, docs); err != nil {
		log.Printf("警告：写入Markdown文件失败: %v", err)
	} else {
		fmt.Printf("成功将DOCX解析结果写入Markdown文件\n")
	}

	// 打印合并后文档的预览
	if len(docs) > 0 {
		mergedDoc := docs[0]
		fmt.Printf("\n========== 合并后的文档内容预览 ==========\n")
		fmt.Printf("文档 ID: %s\n", mergedDoc.ID)
		fmt.Printf("内容长度: %d 字符\n", len(mergedDoc.Content))
		if mergedDoc.MetaData != nil {
			fmt.Printf("元数据: %+v\n", mergedDoc.MetaData)
		}
		// 只显示前500个字符作为预览
		previewLen := 500
		if len(mergedDoc.Content) > previewLen {
			fmt.Printf("内容预览（前%d字符）:\n%s...\n", previewLen, mergedDoc.Content[:previewLen])
		} else {
			fmt.Printf("内容:\n%s\n", mergedDoc.Content)
		}
		fmt.Printf("========================================\n\n")
	}

	// 8. 使用 TFIDF splitter 分割文档，然后使用 LightRAG 进行索引
	fmt.Println("正在使用 TFIDF splitter 和 LightRAG 进行索引...")

	// 调试：显示分割前的文档数量
	fmt.Printf("分割前文档数量: %d\n", len(docs))

	// 先手动调用 transformer 获取分割后的文档，以便打印每个 chunk 的内容
	chunkedDocs, err := splitter.Transform(ctx, docs)
	if err != nil {
		log.Fatalf("分割文档失败: %v", err)
	}

	fmt.Printf("分割后文档块数量: %d\n", len(chunkedDocs))

	// 打印每个 chunk 的完整内容
	fmt.Println("\n========== 开始打印每个 Chunk 的完整内容 ==========")
	for i, chunk := range chunkedDocs {
		fmt.Printf("\n--- Chunk %d/%d ---\n", i+1, len(chunkedDocs))
		fmt.Printf("Chunk ID: %s\n", chunk.ID)
		fmt.Printf("内容长度: %d 字符\n", len(chunk.Content))
		if chunk.MetaData != nil && len(chunk.MetaData) > 0 {
			fmt.Printf("元数据: %+v\n", chunk.MetaData)
		}
		fmt.Printf("内容:\n%s\n", chunk.Content)
		fmt.Printf("--- Chunk %d/%d 结束 ---\n", i+1, len(chunkedDocs))
	}
	fmt.Println("========== 所有 Chunk 内容打印完成 ==========\n")

	// 将文档转换为 map 格式，用于 LightRAG InsertBatch
	documents := make([]map[string]any, 0, len(chunkedDocs))
	for _, doc := range chunkedDocs {
		docMap := make(map[string]any)
		docMap["id"] = doc.ID
		docMap["content"] = doc.Content
		if doc.MetaData != nil {
			for k, v := range doc.MetaData {
				docMap[k] = v
			}
		}
		documents = append(documents, docMap)
	}

	// 使用 LightRAG 的 InsertBatch 方法进行索引（会自动提取知识图谱）
	ids, err := rag.InsertBatch(ctx, documents)
	if err != nil {
		log.Fatalf("索引文档失败: %v", err)
	}

	fmt.Printf("成功索引文档，共生成 %d 个文档块\n", len(ids))

	// 调试：如果生成了文档块，显示前几个块的ID
	if len(ids) > 0 {
		fmt.Printf("前10个文档块ID: %v\n", func() []string {
			if len(ids) > 10 {
				return ids[:10]
			}
			return ids
		}())
	} else {
		fmt.Println("警告：没有生成任何文档块！")
		fmt.Println("可能的原因：")
		fmt.Println("1. 所有文档内容为空")
		fmt.Println("2. TFIDF splitter 分割失败")
		fmt.Println("3. 文档内容格式问题")
	}

	// 等待所有后台的知识图谱提取任务完成
	fmt.Println("\n等待知识图谱提取任务完成...")
	rag.Wait()
	fmt.Println("知识图谱提取完成！")

	fmt.Println("\n索引完成！")
}

// writeDocxToMarkdown 将DOCX解析结果写入Markdown文件
func writeDocxToMarkdown(outputPath string, docxPath string, docs []*schema.Document) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("创建Markdown文件失败: %w", err)
	}
	defer file.Close()

	// 写入文件头
	baseName := filepath.Base(docxPath)
	_, err = fmt.Fprintf(file, "# DOCX解析结果: %s\n\n", baseName)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(file, "**源文件路径**: `%s`\n\n", docxPath)
	if err != nil {
		return err
	}

	// 统计信息
	nonEmptyCount := 0
	totalLength := 0
	for _, doc := range docs {
		if strings.TrimSpace(doc.Content) != "" {
			nonEmptyCount++
			totalLength += len(doc.Content)
		}
	}

	// 判断是否为合并模式
	isMerged := false
	if len(docs) > 0 && docs[0].MetaData != nil {
		if merged, ok := docs[0].MetaData["merged"].(bool); ok && merged {
			isMerged = true
		}
	}

	if isMerged {
		_, err = fmt.Fprintf(file, "**文档模式**: 合并所有章节\n\n")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(file, "**合并后文档数量**: %d\n\n", len(docs))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(file, "**总内容长度**: %d 字符\n\n", totalLength)
	} else {
		_, err = fmt.Fprintf(file, "**总章节数**: %d\n\n", len(docs))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(file, "**有内容的章节数**: %d\n\n", nonEmptyCount)
	}
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(file, "---\n\n")
	if err != nil {
		return err
	}

	// 写入每一章节的内容
	for i, doc := range docs {
		sectionNum := i + 1
		if doc.MetaData != nil {
			if sn, ok := doc.MetaData["section_number"].(int); ok {
				sectionNum = sn
			}
		}

		_, err = fmt.Fprintf(file, "## 第 %d 章节\n\n", sectionNum)
		if err != nil {
			return err
		}

		// 写入元数据
		if doc.MetaData != nil {
			_, err = fmt.Fprintf(file, "**章节ID**: `%s`\n\n", doc.ID)
			if err != nil {
				return err
			}

			if sourceFile, ok := doc.MetaData["source_file"].(string); ok {
				_, err = fmt.Fprintf(file, "**源文件**: `%s`\n\n", sourceFile)
				if err != nil {
					return err
				}
			}
		}

		// 写入内容长度
		_, err = fmt.Fprintf(file, "**内容长度**: %d 字符\n\n", len(doc.Content))
		if err != nil {
			return err
		}

		// 写入内容
		_, err = fmt.Fprintf(file, "### 内容\n\n")
		if err != nil {
			return err
		}

		if strings.TrimSpace(doc.Content) == "" {
			_, err = fmt.Fprintf(file, "*（此章节无内容）*\n\n")
			if err != nil {
				return err
			}
		} else {
			// 将内容写入，使用代码块格式以保持原始格式
			content := doc.Content
			// 转义Markdown特殊字符，或者使用代码块
			_, err = fmt.Fprintf(file, "```\n%s\n```\n\n", content)
			if err != nil {
				return err
			}
		}

		// 章节分隔符
		_, err = fmt.Fprintf(file, "---\n\n")
		if err != nil {
			return err
		}
	}

	return nil
}
