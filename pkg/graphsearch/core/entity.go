package graphsearch

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

// AddEntity 添加实体及其embedding信息
// entityID: 实体唯一标识符
// entityName: 实体名称（用于生成embedding）
// metadata: 实体的元数据
func (g *graphsearch) AddEntity(ctx context.Context, entityID, entityName string, metadata map[string]any) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	if entityID == "" {
		return fmt.Errorf("entityID cannot be empty")
	}

	if entityName == "" {
		entityName = entityID
	}

	// 将metadata序列化为JSON
	var metadataJSON string
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err == nil {
			metadataJSON = string(metadataBytes)
		}
	}

	// 如果提供了embedder，生成embedding
	var embeddingStr string
	embeddingStatus := "pending"
	if g.embedder != nil && entityName != "" {
		embedding, err := g.embedder.Embed(ctx, entityName)
		if err == nil && len(embedding) > 0 {
			// 转换为字符串格式
			vectorStr := "["
			for i, val := range embedding {
				if i > 0 {
					vectorStr += ", "
				}
				vectorStr += fmt.Sprintf("%g", val)
			}
			vectorStr += "]"
			embeddingStr = vectorStr
			embeddingStatus = "completed"
		}
	}

	// 使用 GORM 插入或更新实体
	entity := &Entity{
		EntityID:        entityID,
		EntityName:      entityName,
		Metadata:        metadataJSON,
		Embedding:       embeddingStr,
		EmbeddingStatus: embeddingStatus,
	}

	// 使用 GORM 的 Save 方法实现 upsert（如果主键存在则更新，否则插入）
	if err := g.db.WithContext(ctx).Table(g.tableName).Save(entity).Error; err != nil {
		return fmt.Errorf("failed to add entity: %w", err)
	}

	return nil
}

// GetEntity 获取实体信息
func (g *graphsearch) GetEntity(ctx context.Context, entityID string) (map[string]any, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	var entity Entity
	if err := g.db.WithContext(ctx).Table(g.tableName).Where("entity_id = ?", entityID).First(&entity).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("entity not found: %s", entityID)
		}
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	result := map[string]any{
		"entity_id":        entity.EntityID,
		"entity_name":      entity.EntityName,
		"embedding_status": entity.EmbeddingStatus,
	}

	// 解析metadata
	if entity.Metadata != "" {
		var metadata map[string]any
		if err := json.Unmarshal([]byte(entity.Metadata), &metadata); err == nil {
			result["metadata"] = metadata
		}
	}

	return result, nil
}

// ListEntities 列出所有实体
func (g *graphsearch) ListEntities(ctx context.Context, limit int, offset int) ([]map[string]any, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	if limit <= 0 {
		limit = 100
	}

	var entities []Entity
	if err := g.db.WithContext(ctx).Table(g.tableName).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}

	var results []map[string]any
	for _, entity := range entities {
		result := map[string]any{
			"entity_id":        entity.EntityID,
			"entity_name":      entity.EntityName,
			"embedding_status": entity.EmbeddingStatus,
			"created_at":       entity.CreatedAt,
		}

		// 解析metadata
		if entity.Metadata != "" {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(entity.Metadata), &metadata); err == nil {
				result["metadata"] = metadata
			}
		}

		results = append(results, result)
	}

	return results, nil
}
