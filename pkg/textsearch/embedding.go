package textsearch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	sqlite3_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
)

// processPendingEmbeddings 处理待处理的embedding
func (v *VecStore) processPendingEmbeddings(ctx context.Context) {
	if !v.acquireProcessingLock() {
		return
	}
	defer func() {
		v.isProcessing = false
	}()

	log.Printf("[processPendingEmbeddings] 开始处理待处理的embedding")

	if !v.checkContextAndDB(ctx) {
		return
	}

	processedCount := 0
	for {
		if !v.checkContextAndDB(ctx) {
			log.Printf("[processPendingEmbeddings] Context已取消或数据库已关闭，已处理 %d 个文档", processedCount)
			return
		}

		rows, queryCancel, err := v.queryPendingDocuments(ctx)
		if err != nil {
			return
		}

		if !v.validateQueryColumns(rows) {
			rows.Close()
			queryCancel()
			return
		}

		batchCount, hasRows := v.processBatch(ctx, rows, &processedCount)
		if err := rows.Err(); err != nil {
			log.Printf("[processPendingEmbeddings] 处理行时发生错误: %v", err)
		}

		rows.Close()
		queryCancel()

		if !hasRows {
			log.Printf("[processPendingEmbeddings] 没有查询到待处理的文档，退出循环")
			break
		}

		if batchCount == 0 {
			log.Printf("[processPendingEmbeddings] 本批所有文档处理失败，退出循环")
			break
		}
		log.Printf("[processPendingEmbeddings] 本批处理了 %d 个文档，继续处理下一批", batchCount)
	}

	log.Printf("[processPendingEmbeddings] 处理完成，共处理 %d 个文档", processedCount)
}

// acquireProcessingLock 获取处理锁，防止并发执行
func (v *VecStore) acquireProcessingLock() bool {
	if v.isProcessing {
		log.Printf("[processPendingEmbeddings] 已有处理任务在运行，跳过")
		return false
	}
	v.isProcessing = true
	return true
}

// checkContextAndDB 检查context和数据库连接
func (v *VecStore) checkContextAndDB(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}

	if v.db == nil {
		return false
	}
	return true
}

// queryPendingDocuments 查询待处理的文档
func (v *VecStore) queryPendingDocuments(ctx context.Context) (*sql.Rows, context.CancelFunc, error) {
	queryCtx, queryCancel := context.WithTimeout(ctx, 5*time.Second)
	querySQL := fmt.Sprintf(`
		SELECT id, content, metadata
		FROM %s
		WHERE embedding IS NULL AND embedding_status = 'pending' AND id IS NOT NULL AND length(id) = 16
		LIMIT 10
	`, v.tableName)

	log.Printf("[processPendingEmbeddings] 准备执行查询: %s", querySQL)
	queryStart := time.Now()
	rows, err := v.db.QueryContext(queryCtx, querySQL)
	queryDuration := time.Since(queryStart)

	if err != nil {
		queryCancel()
		if err == context.Canceled || err == context.DeadlineExceeded {
			log.Printf("[processPendingEmbeddings] 查询超时或被取消 (耗时: %v): %v", queryDuration, err)
			return nil, nil, err
		}
		log.Printf("[processPendingEmbeddings] 查询失败 (耗时: %v): %v", queryDuration, err)
		return nil, nil, err
	}

	log.Printf("[processPendingEmbeddings] 查询成功 (耗时: %v)，开始处理结果", queryDuration)
	return rows, queryCancel, nil
}

// validateQueryColumns 验证查询返回的列
func (v *VecStore) validateQueryColumns(rows *sql.Rows) bool {
	columns, err := rows.Columns()
	if err != nil {
		log.Printf("[processPendingEmbeddings] 获取列信息失败: %v", err)
		return false
	}

	if len(columns) != 3 {
		log.Printf("[processPendingEmbeddings] 查询返回的列数不正确: 期望 3 列，实际 %d 列，列名: %v", len(columns), columns)
		return false
	}

	log.Printf("[processPendingEmbeddings] 查询返回 %d 列: %v", len(columns), columns)
	return true
}

// processBatch 处理一批文档
func (v *VecStore) processBatch(ctx context.Context, rows *sql.Rows, processedCount *int) (int, bool) {
	batchCount := 0
	hasRows := false

	for rows.Next() {
		hasRows = true

		if !v.checkContextAndDB(ctx) {
			log.Printf("[processPendingEmbeddings] 处理过程中Context已取消或数据库已关闭，已处理 %d 个文档", *processedCount)
			return batchCount, hasRows
		}

		id, content, ok := v.scanDocumentRow(rows)
		if !ok || id == "" {
			continue
		}

		log.Printf("[processPendingEmbeddings] 开始处理文档 ID: %s", id)

		success := v.processDocument(ctx, id, content)
		if success {
			(*processedCount)++
			batchCount++
		}
	}

	return batchCount, hasRows
}

// scanDocumentRow 扫描文档行并提取id、content和metadata
func (v *VecStore) scanDocumentRow(rows *sql.Rows) (id, content string, ok bool) {
	columns, err := rows.Columns()
	if err != nil {
		return "", "", false
	}

	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

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
		return "", "", false
	}

	// 转换id（从二进制 UUID 转换为字符串）
	if len(values) >= 1 && values[0] != nil {
		var idBytes []byte
		switch v := values[0].(type) {
		case []byte:
			idBytes = v
		case string:
			// 兼容旧数据：如果是字符串，尝试解析
			idBytes = []byte(v)
		default:
			log.Printf("[processPendingEmbeddings] id 字段类型异常: %T, 值: %v", v, v)
			return "", "", false
		}

		// 将二进制 UUID 转换为字符串
		if len(idBytes) == 16 {
			idStr, err := sqlite3_driver.BytesToUUIDString(idBytes)
			if err != nil {
				log.Printf("[processPendingEmbeddings] 无效的 UUID 二进制数据: %v", err)
				return "", "", false
			}
			id = idStr
		} else {
			log.Printf("[processPendingEmbeddings] id 长度不正确: 期望 16 字节，实际 %d 字节", len(idBytes))
			return "", "", false
		}
	} else {
		log.Printf("[processPendingEmbeddings] id 字段为 nil")
		return "", "", false
	}

	// 转换content
	if len(values) >= 2 && values[1] != nil {
		switch v := values[1].(type) {
		case string:
			content = v
		case []byte:
			content = string(v)
		default:
			content = fmt.Sprintf("%v", v)
			log.Printf("[processPendingEmbeddings] content 字段类型异常: %T", v)
		}
	}

	if id == "" {
		log.Printf("[processPendingEmbeddings] 跳过无效行（id 为空）")
		return "", "", false
	}

	return id, content, true
}

// processDocument 处理单个文档：解析、提取文本、生成embedding并保存
func (v *VecStore) processDocument(ctx context.Context, id, content string) bool {
	// 解析文档
	doc, err := v.parseDocument(id, content)
	if err != nil {
		v.updateDocumentStatus(ctx, id, "failed")
		return false
	}

	// 提取文本内容
	textContent := v.extractTextContent(doc, id)
	if textContent == "" {
		v.updateDocumentStatus(ctx, id, "failed")
		return false
	}

	log.Printf("[processPendingEmbeddings] 文档内容长度: %d 字符 (ID: %s)", len(textContent), id)

	if !v.checkContextAndDB(ctx) {
		return false
	}

	// 更新状态为processing
	v.updateDocumentStatus(ctx, id, "processing")
	log.Printf("[processPendingEmbeddings] 更新文档状态为 processing (ID: %s)", id)

	// 生成并保存embedding
	return v.generateAndSaveEmbedding(ctx, id, textContent)
}

// parseDocument 解析文档JSON
func (v *VecStore) parseDocument(id, content string) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		log.Printf("[processPendingEmbeddings] 解析文档失败 (ID: %s): %v", id, err)
		return nil, err
	}
	return doc, nil
}

// extractTextContent 从文档中提取文本内容
func (v *VecStore) extractTextContent(doc map[string]any, id string) string {
	textContent := ""
	if text, ok := doc["content"].(string); ok {
		textContent = text
	}
	if textContent == "" {
		log.Printf("[processPendingEmbeddings] 文档内容为空 (ID: %s)，跳过并标记为failed", id)
	}
	return textContent
}

// generateAndSaveEmbedding 生成embedding并保存到数据库
func (v *VecStore) generateAndSaveEmbedding(ctx context.Context, id, textContent string) bool {
	if v.embedder == nil {
		log.Printf("[processPendingEmbeddings] Embedder未设置，跳过embedding生成 (ID: %s)", id)
		v.updateDocumentStatus(ctx, id, "failed")
		return false
	}

	if !v.checkContextAndDB(ctx) {
		return false
	}

	log.Printf("[processPendingEmbeddings] 开始生成embedding (ID: %s)", id)

	// 尝试使用 text-embedding-v4 模型
	embedding, err := v.embedWithV4Model(ctx, textContent)
	if err != nil || len(embedding) == 0 {
		log.Printf("[processPendingEmbeddings] Embedding生成失败 (ID: %s): %v", id, err)
		v.updateDocumentStatus(ctx, id, "failed")
		return false
	}

	log.Printf("[processPendingEmbeddings] Embedding生成成功 (ID: %s, 维度: %d)", id, len(embedding))

	// 将向量序列化为 JSON 格式存储为 BLOB
	embeddingJSON, err := json.Marshal(embedding)
	if err != nil {
		log.Printf("[processPendingEmbeddings] 序列化embedding失败 (ID: %s): %v", id, err)
		v.updateDocumentStatus(ctx, id, "failed")
		return false
	}

	if !v.checkContextAndDB(ctx) {
		log.Printf("[processPendingEmbeddings] 数据库连接已关闭，无法保存embedding (ID: %s)", id)
		return false
	}

	// 将 UUID 字符串转换为二进制
	idBytes, err := sqlite3_driver.UUIDStringToBytes(id)
	if err != nil {
		log.Printf("[generateAndSaveEmbedding] 无效的 UUID 字符串: %v", err)
		v.updateDocumentStatus(ctx, id, "failed")
		return false
	}

	// 更新向量列和状态
	updateCtx, updateCancel := context.WithTimeout(ctx, 1*time.Second)
	updateVectorSQL := fmt.Sprintf(`UPDATE %s SET embedding = ?, embedding_status = 'completed' WHERE id = ?`, v.tableName)
	_, _ = v.db.ExecContext(updateCtx, updateVectorSQL, embeddingJSON, idBytes)
	updateCancel()

	log.Printf("[processPendingEmbeddings] 文档处理完成 (ID: %s)", id)
	return true
}

// updateDocumentStatus 更新文档状态
func (v *VecStore) updateDocumentStatus(ctx context.Context, id, status string) {
	if v.db == nil {
		return
	}

	// 将 UUID 字符串转换为二进制
	idBytes, err := sqlite3_driver.UUIDStringToBytes(id)
	if err != nil {
		log.Printf("[updateDocumentStatus] 无效的 UUID 字符串: %v", err)
		return
	}

	updateCtx, updateCancel := context.WithTimeout(ctx, 1*time.Second)
	defer updateCancel()

	var updateSQL string
	if status == "processing" {
		updateSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = 'processing' WHERE id = ? AND embedding IS NULL AND embedding_status = 'pending'`, v.tableName)
	} else {
		updateSQL = fmt.Sprintf(`UPDATE %s SET embedding_status = ? WHERE id = ?`, v.tableName)
	}

	if status == "processing" {
		_, _ = v.db.ExecContext(updateCtx, updateSQL, idBytes)
	} else {
		_, _ = v.db.ExecContext(updateCtx, updateSQL, status, idBytes)
	}
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
