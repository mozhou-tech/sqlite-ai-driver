package textsearch

import (
	"context"
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

	if !v.checkContextAndDB(ctx) {
		return
	}

	processedCount := 0
	for {
		if !v.checkContextAndDB(ctx) {
			return
		}

		documents, queryCancel, err := v.queryPendingDocuments(ctx)
		if err != nil {
			queryCancel()
			return
		}

		if !v.validateQueryColumns(documents) {
			queryCancel()
			return
		}

		batchCount, hasRows := v.processBatch(ctx, documents, &processedCount)
		queryCancel()

		if !hasRows {
			break
		}

		if batchCount == 0 {
			break
		}
	}

	if processedCount > 0 {
		log.Printf("[processPendingEmbeddings] 处理完成，共处理 %d 个文档", processedCount)
	}
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
// 返回文档列表和取消函数
func (v *VecStore) queryPendingDocuments(ctx context.Context) ([]Document, context.CancelFunc, error) {
	queryCtx, queryCancel := context.WithTimeout(ctx, 5*time.Second)

	var documents []Document
	err := v.db.WithContext(queryCtx).
		Where("embedding IS NULL AND embedding_status = ? AND id IS NOT NULL AND length(id) = ?", "pending", 16).
		Limit(10).
		Find(&documents).Error

	if err != nil {
		queryCancel()
		if err != context.Canceled && err != context.DeadlineExceeded {
			log.Printf("[processPendingEmbeddings] 查询失败: %v", err)
		}
		return nil, nil, err
	}

	return documents, queryCancel, nil
}

// validateQueryColumns 验证查询返回的文档（GORM 版本，不再需要验证列）
func (v *VecStore) validateQueryColumns(documents []Document) bool {
	return documents != nil
}

// processBatch 处理一批文档
func (v *VecStore) processBatch(ctx context.Context, documents []Document, processedCount *int) (int, bool) {
	batchCount := 0
	hasRows := len(documents) > 0

	for _, doc := range documents {
		if !v.checkContextAndDB(ctx) {
			return batchCount, hasRows
		}

		// 将二进制 UUID 转换为字符串
		id, err := sqlite3_driver.BytesToUUIDString(doc.ID)
		if err != nil {
			log.Printf("[processPendingEmbeddings] 无效的 UUID: %v", err)
			continue
		}

		if id == "" {
			continue
		}

		success := v.processDocument(ctx, id, doc.Content)
		if success {
			(*processedCount)++
			batchCount++
		}
	}

	return batchCount, hasRows
}

// scanDocumentRow 已废弃，GORM 版本不再需要此方法
// 保留此方法签名以保持兼容性，但实际不再使用
func (v *VecStore) scanDocumentRow(doc Document) (id, content string, ok bool) {
	// 将二进制 UUID 转换为字符串
	id, err := sqlite3_driver.BytesToUUIDString(doc.ID)
	if err != nil {
		log.Printf("[processPendingEmbeddings] 无效的 UUID 二进制数据: %v", err)
		return "", "", false
	}

	if id == "" {
		return "", "", false
	}

	return id, doc.Content, true
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

	if !v.checkContextAndDB(ctx) {
		return false
	}

	// 更新状态为processing
	v.updateDocumentStatus(ctx, id, "processing")

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

	// 尝试使用 text-embedding-v4 模型
	embedding, err := v.embedWithV4Model(ctx, textContent)
	if err != nil || len(embedding) == 0 {
		log.Printf("[processPendingEmbeddings] Embedding生成失败 (ID: %s): %v", id, err)
		v.updateDocumentStatus(ctx, id, "failed")
		return false
	}

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

	// 更新向量列和状态，使用 GORM Update
	updateCtx, updateCancel := context.WithTimeout(ctx, 1*time.Second)
	defer updateCancel()

	err = v.db.WithContext(updateCtx).
		Model(&Document{}).
		Where("id = ?", idBytes).
		Updates(map[string]interface{}{
			"embedding":        embeddingJSON,
			"embedding_status": "completed",
		}).Error

	if err != nil {
		log.Printf("[generateAndSaveEmbedding] 更新文档失败 (ID: %s): %v", id, err)
		return false
	}

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

	if status == "processing" {
		// 只有在 embedding 为 NULL 且状态为 pending 时才更新为 processing
		err = v.db.WithContext(updateCtx).
			Model(&Document{}).
			Where("id = ? AND embedding IS NULL AND embedding_status = ?", idBytes, "pending").
			Update("embedding_status", "processing").Error
	} else {
		err = v.db.WithContext(updateCtx).
			Model(&Document{}).
			Where("id = ?", idBytes).
			Update("embedding_status", status).Error
	}

	if err != nil {
		log.Printf("[updateDocumentStatus] 更新文档状态失败 (ID: %s, Status: %s): %v", id, status, err)
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
	return v.embedder.Embed(ctx, text)
}
