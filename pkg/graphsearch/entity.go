package graphsearch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
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

	// 插入或更新实体
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (entity_id, entity_name, metadata, embedding, embedding_status, updated_at)
		VALUES (?, ?, ?, %s, ?, CURRENT_TIMESTAMP)
		ON CONFLICT (entity_id) DO UPDATE SET
			entity_name = excluded.entity_name,
			metadata = excluded.metadata,
			embedding = excluded.embedding,
			embedding_status = excluded.embedding_status,
			updated_at = CURRENT_TIMESTAMP
	`, g.tableName, func() string {
		if embeddingStr != "" {
			return "?::FLOAT[]"
		}
		return "NULL"
	}())

	var err error
	if embeddingStr != "" {
		_, err = g.db.ExecContext(ctx, insertSQL, entityID, entityName, metadataJSON, embeddingStr, embeddingStatus)
	} else {
		_, err = g.db.ExecContext(ctx, insertSQL, entityID, entityName, metadataJSON, embeddingStatus)
	}

	if err != nil {
		return fmt.Errorf("failed to add entity: %w", err)
	}

	return nil
}

// GetEntity 获取实体信息
func (g *graphsearch) GetEntity(ctx context.Context, entityID string) (map[string]any, error) {
	if !g.initialized {
		return nil, fmt.Errorf("store not initialized, call Initialize first")
	}

	querySQL := fmt.Sprintf(`
		SELECT entity_id, entity_name, metadata, embedding_status
		FROM %s
		WHERE entity_id = ?
	`, g.tableName)

	row := g.db.QueryRowContext(ctx, querySQL, entityID)

	var id, name string
	var metadataVal any
	var embeddingStatus string

	err := row.Scan(&id, &name, &metadataVal, &embeddingStatus)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entity not found: %s", entityID)
		}
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	result := map[string]any{
		"entity_id":        id,
		"entity_name":      name,
		"embedding_status": embeddingStatus,
	}

	// 解析metadata
	if metadataVal != nil {
		if metadataStr, ok := metadataVal.(string); ok {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil {
				result["metadata"] = metadata
			}
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

	querySQL := fmt.Sprintf(`
		SELECT entity_id, entity_name, metadata, embedding_status, created_at
		FROM %s
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, g.tableName)

	rows, err := g.db.QueryContext(ctx, querySQL, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, name string
		var metadataVal any
		var embeddingStatus string
		var createdAt time.Time

		err := rows.Scan(&id, &name, &metadataVal, &embeddingStatus, &createdAt)
		if err != nil {
			continue
		}

		result := map[string]any{
			"entity_id":        id,
			"entity_name":      name,
			"embedding_status": embeddingStatus,
			"created_at":       createdAt,
		}

		// 解析metadata
		if metadataVal != nil {
			if metadataStr, ok := metadataVal.(string); ok {
				var metadata map[string]any
				if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil {
					result["metadata"] = metadata
				}
			}
		}

		results = append(results, result)
	}

	return results, nil
}
