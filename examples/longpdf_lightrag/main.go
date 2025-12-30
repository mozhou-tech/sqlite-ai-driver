package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/schema"
	pdfparser "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/document/parser/pdf"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/document/transformer/splitter/tfidf"
	lightragindexer "github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext/indexer/lightrag"
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

	// 2. 设置工作目录和 PDF 文件路径
	workingDir := "./rag_storage"
	_ = os.MkdirAll(workingDir, 0755)

	pdfPath := "/Users/mozhou/WorkSpace/supacloud/测试数据/淮河干支线数字化转型升级施工项目/招标文件与设计文件/淮河数字化连云港设计文件终稿.pdf"

	// 检查文件是否存在
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		log.Fatalf("PDF 文件不存在: %s", pdfPath)
	}

	// 3. 初始化 Embedder（使用 eino 的 embedding 接口）
	embedder, err := openaiembedding.NewEmbedder(ctx, &openaiembedding.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   "text-embedding-v4",
	})
	if err != nil {
		log.Fatalf("创建 embedder 失败: %v", err)
	}

	// 4. 创建 TFIDF Splitter
	splitter, err := tfidf.NewTFIDFSplitter(ctx, &tfidf.Config{
		SimilarityThreshold:  0.2,
		MaxChunkSize:         800,  // 800 字符
		MinChunkSize:         700,  // 700 字符
		MaxSentencesPerChunk: 50,   // 最多 50 个句子
		UseSego:              true, // 使用 sego 进行中文分词
		IDGenerator: func(ctx context.Context, originalID string, splitIndex int) string {
			return fmt.Sprintf("%s_chunk_%d", originalID, splitIndex)
		},
	})
	if err != nil {
		log.Fatalf("创建 TFIDF splitter 失败: %v", err)
	}

	// 5. 创建 LightRAG Indexer，并传入 TFIDF splitter 作为 transformer
	// 使用 pkg/eino-ext/indexer/lightrag 中的 LightRAG 实现（使用 DuckDB 和 Cayley graph）
	lightRAGInstance, err := lightragindexer.New(lightragindexer.Options{
		DuckDBPath: filepath.Join(workingDir, "lightrag.duckdb"),
		GraphPath:  filepath.Join(workingDir, "lightrag.graph.db"),
		Embedder:   embedder,
		TableName:  "documents",
	})
	if err != nil {
		log.Fatalf("创建 LightRAG 实例失败: %v", err)
	}
	defer lightRAGInstance.Close()

	// 6. 解析 PDF 文件（合并所有页面）
	fmt.Printf("正在解析 PDF 文件: %s\n", pdfPath)
	pdfParser, err := pdfparser.NewPDFParser(ctx, &pdfparser.Config{
		ToPages: false, // 合并所有页面为一个文档
	})
	if err != nil {
		log.Fatalf("创建 PDF 解析器失败: %v", err)
	}

	// 打开 PDF 文件
	pdfFile, err := os.Open(pdfPath)
	if err != nil {
		log.Fatalf("打开 PDF 文件失败: %v", err)
	}
	defer pdfFile.Close()

	// 解析 PDF（合并所有页面）
	var docs []*schema.Document
	docs, err = pdfParser.Parse(ctx, pdfFile)
	if err != nil {
		if len(docs) == 0 {
			log.Fatalf("解析 PDF 完全失败: %v", err)
		}
		log.Printf("警告：PDF 解析部分失败: %v，但已成功解析部分内容，将继续处理", err)
	}

	if len(docs) == 0 {
		log.Fatalf("解析 PDF 后没有生成任何文档")
	}

	// 合并后应该只有一个文档
	if len(docs) > 1 {
		log.Printf("警告：合并模式应该只生成一个文档，但生成了 %d 个文档", len(docs))
	}

	// 为合并后的文档设置 ID 和元数据
	baseID := filepath.Base(pdfPath)
	doc := docs[0]
	if doc.ID == "" {
		doc.ID = baseID
	}
	if doc.MetaData == nil {
		doc.MetaData = make(map[string]any)
	}
	doc.MetaData["source_file"] = pdfPath
	doc.MetaData["merged"] = true

	fmt.Printf("成功解析 PDF，合并后文档长度: %d 字符\n", len(doc.Content))

	// 将PDF解析结果输出到Markdown文件
	mdOutputPath := filepath.Join(workingDir, "pdf_parsed_output.md")
	fmt.Printf("正在将PDF解析结果写入Markdown文件: %s\n", mdOutputPath)
	if err := writePDFToMarkdown(mdOutputPath, pdfPath, docs); err != nil {
		log.Printf("警告：写入Markdown文件失败: %v", err)
	} else {
		fmt.Printf("成功将PDF解析结果写入Markdown文件\n")
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

	// 7. 使用 Indexer 索引文档
	fmt.Println("正在使用 TFIDF splitter 和 LightRAG indexer 进行索引...")

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

	// 创建一个不包含 transformer 的 indexer，使用已经分割好的文档进行索引
	// 这样可以避免重复分割
	indexer, err := lightragindexer.NewIndexer(ctx, &lightragindexer.IndexerConfig{
		LightRAG:    lightRAGInstance,
		Transformer: nil, // 不配置 transformer，因为文档已经分割好了
	})
	if err != nil {
		log.Fatalf("创建 indexer 失败: %v", err)
	}

	// 使用已经分割好的文档进行索引
	ids, err := indexer.Store(ctx, chunkedDocs)
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

	fmt.Println("索引完成！")
}

// writePDFToMarkdown 将PDF解析结果写入Markdown文件
func writePDFToMarkdown(outputPath string, pdfPath string, docs []*schema.Document) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("创建Markdown文件失败: %w", err)
	}
	defer file.Close()

	// 写入文件头
	baseName := filepath.Base(pdfPath)
	_, err = fmt.Fprintf(file, "# PDF解析结果: %s\n\n", baseName)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(file, "**源文件路径**: `%s`\n\n", pdfPath)
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
		_, err = fmt.Fprintf(file, "**文档模式**: 合并所有页面\n\n")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(file, "**合并后文档数量**: %d\n\n", len(docs))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(file, "**总内容长度**: %d 字符\n\n", totalLength)
	} else {
		_, err = fmt.Fprintf(file, "**总页数**: %d\n\n", len(docs))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(file, "**有内容的页数**: %d\n\n", nonEmptyCount)
	}
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(file, "---\n\n")
	if err != nil {
		return err
	}

	// 写入每一页的内容
	for i, doc := range docs {
		pageNum := i + 1
		if doc.MetaData != nil {
			if pn, ok := doc.MetaData["page_number"].(int); ok {
				pageNum = pn
			}
		}

		_, err = fmt.Fprintf(file, "## 第 %d 页\n\n", pageNum)
		if err != nil {
			return err
		}

		// 写入元数据
		if doc.MetaData != nil {
			_, err = fmt.Fprintf(file, "**页面ID**: `%s`\n\n", doc.ID)
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
			_, err = fmt.Fprintf(file, "*（此页无内容）*\n\n")
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

		// 页面分隔符
		_, err = fmt.Fprintf(file, "---\n\n")
		if err != nil {
			return err
		}
	}

	return nil
}
