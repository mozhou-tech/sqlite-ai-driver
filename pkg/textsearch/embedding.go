package textsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
)

// processPendingEmbeddings 处理待处理的embedding
func (v *VecStore) processPendingEmbeddings(ctx context.Context) {
	// 防止并发执行（使用非阻塞方式）
	if v.isProcessing {
		log.Printf("[processPendingEmbeddings] 已有处理任务在运行，跳过")
		return
	}
	v.isProcessing = true

	// 确保在函数退出时重置标志
	defer func() {
		v.isProcessing = false
	}()

	log.Printf("[processPendingEmbeddings] 开始处理待处理的embedding")

	// 检查 context 是否已取消
	select {
	case <-ctx.Done():
		log.Printf("[processPendingEmbeddings] Context已取消，退出")
		return
	default:
	}

	// 检查数据库是否已关闭
	db := v.db
	if db == nil {
		log.Printf("[processPendingEmbeddings] 数据库连接为空，退出")
		return
	}

	processedCount := 0
	// 循环处理，直到没有更多待处理的文档
	for {
		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			log.Printf("[processPendingEmbeddings] Context已取消，已处理 %d 个文档", processedCount)
			return
		default:
		}

		// 检查数据库是否已关闭
		db = v.db
		if db == nil {
			log.Printf("[processPendingEmbeddings] 数据库连接已关闭，退出")
			return
		}

		// 查询pending状态的文档（使用带超时的 context，避免无限阻塞）
		queryCtx, queryCancel := context.WithTimeout(ctx, 5*time.Second)
		querySQL := fmt.Sprintf(`
			SELECT id, content, metadata
			FROM %s
			WHERE embedding IS NULL AND embedding_status = 'pending' AND id IS NOT NULL AND id != ''
			LIMIT 10
		`, v.tableName)

		log.Printf("[processPendingEmbeddings] 准备执行查询: %s", querySQL)
		queryStart := time.Now()
		rows, err := db.QueryContext(queryCtx, querySQL)
		queryDuration := time.Since(queryStart)
		if err != nil {
			queryCancel()
			// 如果是 context 取消或超时，直接返回
			if err == context.Canceled || err == context.DeadlineExceeded {
				log.Printf("[processPendingEmbeddings] 查询超时或被取消 (耗时: %v): %v", queryDuration, err)
				return
			}
			log.Printf("[processPendingEmbeddings] 查询失败 (耗时: %v): %v", queryDuration, err)
			return
		}
		log.Printf("[processPendingEmbeddings] 查询成功 (耗时: %v)，开始处理结果", queryDuration)

		// 检查查询返回的列数
		columns, err := rows.Columns()
		if err != nil {
			log.Printf("[processPendingEmbeddings] 获取列信息失败: %v", err)
			rows.Close()
			queryCancel()
			return
		}
		if len(columns) != 3 {
			log.Printf("[processPendingEmbeddings] 查询返回的列数不正确: 期望 3 列，实际 %d 列，列名: %v", len(columns), columns)
			rows.Close()
			queryCancel()
			return
		}
		log.Printf("[processPendingEmbeddings] 查询返回 %d 列: %v", len(columns), columns)

		batchCount := 0
		hasRows := false
		defer func() {
			rows.Close()
			queryCancel()
		}()

		for rows.Next() {
			hasRows = true
			// 检查 context 是否已取消
			select {
			case <-ctx.Done():
				log.Printf("[processPendingEmbeddings] 处理过程中Context已取消，已处理 %d 个文档", processedCount)
				return
			default:
			}

			// 使用更安全的方式扫描：先扫描到 interface{} 切片，然后转换
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			// 使用 recover 捕获可能的 panic
			scanErr := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[processPendingEmbeddings] 扫描行时发生 panic: %v, 列数: %d", r, len(columns))
						err = fmt.Errorf("panic during scan: %v", r)
					}
				}()
				return rows.Scan(valuePtrs...)
			}()

			if scanErr != nil {
				log.Printf("[processPendingEmbeddings] 扫描行失败: %v", scanErr)
				continue
			}

			// 转换值
			var id, content string
			var metadataVal any

			if len(values) >= 1 && values[0] != nil {
				switch v := values[0].(type) {
				case string:
					id = v
				case []byte:
					id = string(v)
				default:
					// 尝试通过 fmt.Sprintf 转换
					id = fmt.Sprintf("%v", v)
					log.Printf("[processPendingEmbeddings] id 字段类型异常: %T, 值: %v", v, v)
				}
			} else if len(values) >= 1 {
				log.Printf("[processPendingEmbeddings] id 字段为 nil，values[0] 类型: %T", values[0])
			}

			if len(values) >= 2 && values[1] != nil {
				switch v := values[1].(type) {
				case string:
					content = v
				case []byte:
					content = string(v)
				default:
					// 尝试通过 fmt.Sprintf 转换
					content = fmt.Sprintf("%v", v)
					log.Printf("[processPendingEmbeddings] content 字段类型异常: %T", v)
				}
			}
			if len(values) >= 3 {
				metadataVal = values[2]
			}

			// metadataVal 被扫描但当前未使用，保留以备将来使用
			_ = metadataVal

			// 如果 id 为空，说明无法从扫描结果中提取有效的 id 值，跳过这一行
			// 可能原因：id 字段为 NULL、类型转换失败、或值为空字符串
			if id == "" {
				var idValue interface{}
				if len(values) >= 1 {
					idValue = values[0]
				}
				log.Printf("[processPendingEmbeddings] 跳过无效行（id 为空），values[0] 类型: %T, 值: %v", idValue, idValue)
				continue
			}

			log.Printf("[processPendingEmbeddings] 开始处理文档 ID: %s", id)

			// 解析文档
			var doc map[string]any
			if err := json.Unmarshal([]byte(content), &doc); err != nil {
				log.Printf("[processPendingEmbeddings] 解析文档失败 (ID: %s): %v", id, err)
				// 解析失败时，更新状态为failed，避免重复查询
				updateCtx, updateCancel := context.WithTimeout(ctx, 1*time.Second)
				updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
				_, _ = db.ExecContext(updateCtx, updateStatusSQL, id)
				updateCancel()
				continue
			}

			// 提取文本内容
			textContent := ""
			if text, ok := doc["content"].(string); ok {
				textContent = text
			}
			if textContent == "" {
				log.Printf("[processPendingEmbeddings] 文档内容为空 (ID: %s)，跳过并标记为failed", id)
				// 内容为空时，更新状态为failed，避免重复查询
				updateCtx, updateCancel := context.WithTimeout(ctx, 1*time.Second)
				updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
				_, _ = db.ExecContext(updateCtx, updateStatusSQL, id)
				updateCancel()
				continue
			}

			log.Printf("[processPendingEmbeddings] 文档内容长度: %d 字符 (ID: %s)", len(textContent), id)

			// 检查数据库是否已关闭
			db = v.db
			if db == nil {
				log.Printf("[processPendingEmbeddings] 数据库连接已关闭，退出处理")
				queryCancel()
				return
			}

			// 检查 context 是否已取消
			select {
			case <-ctx.Done():
				log.Printf("[processPendingEmbeddings] Context已取消，退出处理")
				queryCancel()
				return
			default:
			}

			// 更新状态为processing（使用带超时的 context）
			updateCtx, updateCancel := context.WithTimeout(ctx, 1*time.Second)
			updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'processing' WHERE id = ? AND embedding IS NULL AND embedding_status = 'pending'`, v.tableName)
			_, _ = db.ExecContext(updateCtx, updateStatusSQL, id)
			updateCancel()
			log.Printf("[processPendingEmbeddings] 更新文档状态为 processing (ID: %s)", id)

			// 生成embedding
			if v.embedder != nil {
				// 检查 context 是否已取消
				select {
				case <-ctx.Done():
					log.Printf("[processPendingEmbeddings] Context已取消，退出embedding生成")
					queryCancel()
					return
				default:
				}

				// 再次检查 context 是否已取消
				select {
				case <-ctx.Done():
					log.Printf("[processPendingEmbeddings] Context已取消，退出embedding生成")
					queryCancel()
					return
				default:
				}

				log.Printf("[processPendingEmbeddings] 开始生成embedding (ID: %s)", id)

				// 尝试使用 text-embedding-v4 模型
				embedding, err := v.embedWithV4Model(ctx, textContent)
				if err == nil && len(embedding) > 0 {
					log.Printf("[processPendingEmbeddings] Embedding生成成功 (ID: %s, 维度: %d)", id, len(embedding))
					// 将向量序列化为 JSON 格式存储为 BLOB
					embeddingJSON, err := json.Marshal(embedding)
					if err == nil {
						// 再次检查数据库是否已关闭
						db = v.db
						if db == nil {
							log.Printf("[processPendingEmbeddings] 数据库连接已关闭，无法保存embedding (ID: %s)", id)
							queryCancel()
							return
						}
						// 更新向量列和状态（使用带超时的 context）
						updateCtx2, updateCancel2 := context.WithTimeout(ctx, 1*time.Second)
						updateVectorSQL := fmt.Sprintf(`UPDATE %s SET embedding = ?, embedding_status = 'completed' WHERE id = ?`, v.tableName)
						_, _ = db.ExecContext(updateCtx2, updateVectorSQL, embeddingJSON, id)
						updateCancel2()
						log.Printf("[processPendingEmbeddings] 文档处理完成 (ID: %s)", id)
						processedCount++
						batchCount++
					} else {
						log.Printf("[processPendingEmbeddings] 序列化embedding失败 (ID: %s): %v", id, err)
						// 更新状态为failed（使用带超时的 context）
						updateCtx3, updateCancel3 := context.WithTimeout(ctx, 1*time.Second)
						updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
						_, _ = db.ExecContext(updateCtx3, updateStatusSQL, id)
						updateCancel3()
					}
				} else {
					log.Printf("[processPendingEmbeddings] Embedding生成失败 (ID: %s): %v", id, err)
					// 更新状态为failed（使用带超时的 context）
					updateCtx4, updateCancel4 := context.WithTimeout(ctx, 1*time.Second)
					updateStatusSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
					_, _ = db.ExecContext(updateCtx4, updateStatusSQL, id)
					updateCancel4()
				}
			} else {
				log.Printf("[processPendingEmbeddings] Embedder未设置，跳过embedding生成 (ID: %s)", id)
				// Embedder未设置时，更新状态为failed，避免重复查询
				updateCtx, updateCancel := context.WithTimeout(ctx, 1*time.Second)
				updateStatusSQL := fmt.Sprintf(`UPDATE %s SET embedding_status = 'failed' WHERE id = ?`, v.tableName)
				_, _ = db.ExecContext(updateCtx, updateStatusSQL, id)
				updateCancel()
			}
		}

		// 检查 rows 是否有错误
		if err := rows.Err(); err != nil {
			log.Printf("[processPendingEmbeddings] 处理行时发生错误: %v", err)
			// 即使有错误，也继续处理，但记录错误
		}

		// defer 中已经处理了 rows.Close() 和 queryCancel()

		// 如果这一批没有查询到任何文档，说明没有更多待处理的文档了
		if !hasRows {
			log.Printf("[processPendingEmbeddings] 没有查询到待处理的文档，退出循环")
			break
		}

		// 如果这一批没有成功处理任何文档（所有文档都失败了），也退出循环，避免无限循环
		if batchCount == 0 {
			log.Printf("[processPendingEmbeddings] 本批所有文档处理失败，退出循环")
			break
		}
		log.Printf("[processPendingEmbeddings] 本批处理了 %d 个文档，继续处理下一批", batchCount)
	}

	log.Printf("[processPendingEmbeddings] 处理完成，共处理 %d 个文档", processedCount)
}

// embedWithV4Model 使用 text-embedding-v4 模型生成 embedding
// 如果环境变量 OPENAI_API_KEY 存在，则使用 Eino 的 OpenAI embedder
// 否则使用原来的 embedder
func (v *VecStore) embedWithV4Model(ctx context.Context, text string) ([]float64, error) {
	// 尝试从环境变量获取配置
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey != "" {
		// 如果环境变量存在，使用 Eino 的 OpenAI embedder
		baseURL := os.Getenv("OPENAI_BASE_URL")
		model := os.Getenv("OPENAI_EMBEDDING_MODEL")
		if model == "" {
			model = "text-embedding-v4" // 默认使用 text-embedding-v4 模型
		}

		// 配置 Eino OpenAI embedder
		config := &openai.EmbeddingConfig{
			APIKey:  apiKey,
			Model:   model,
			Timeout: 30 * time.Second,
		}

		// 如果设置了自定义 baseURL，使用它（支持 Azure OpenAI）
		if baseURL != "" {
			config.BaseURL = baseURL
			// 检查是否是 Azure OpenAI Service
			if os.Getenv("OPENAI_USE_AZURE") == "true" {
				config.ByAzure = true
				apiVersion := os.Getenv("OPENAI_API_VERSION")
				if apiVersion == "" {
					apiVersion = "2023-05-15"
				}
				config.APIVersion = apiVersion
			}
		}

		log.Printf("[embedWithV4Model] 使用 Eino OpenAI embedder (model: %s) 生成 embedding", model)
		embedder, err := openai.NewEmbedder(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI embedder: %w", err)
		}

		// 使用 EmbedStrings 方法生成 embedding
		embeddings, err := embedder.EmbedStrings(ctx, []string{text})
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding: %w", err)
		}

		if len(embeddings) == 0 {
			return nil, fmt.Errorf("no embedding returned")
		}

		return embeddings[0], nil
	}

	// 如果环境变量不存在，使用原来的 embedder
	log.Printf("[embedWithV4Model] 使用原来的 embedder")
	return v.embedder.Embed(ctx, text)
}
