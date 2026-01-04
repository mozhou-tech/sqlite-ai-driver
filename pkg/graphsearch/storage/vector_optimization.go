package storage

import (
	"math"
	"sync"
)

// VectorSearchOptimizer 向量检索优化器
type VectorSearchOptimizer struct {
	cache      map[string][]float64
	cacheMutex sync.RWMutex
	batchSize  int
}

// NewVectorSearchOptimizer 创建向量检索优化器
func NewVectorSearchOptimizer(batchSize int) *VectorSearchOptimizer {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &VectorSearchOptimizer{
		cache:     make(map[string][]float64),
		batchSize: batchSize,
	}
}

// 注意：以下函数需要访问 graphsearch 类型的私有字段，因此应该在 graphsearch 包中定义。
// 这些函数应该移动到 core 目录中的新文件，或者添加到现有的 core 文件中。
// 暂时注释掉以避免编译错误。

// candidateResult 候选结果
type candidateResult struct {
	entityID   string
	entityName string
	metadata   map[string]any
	similarity float64
}

// batchComputeSimilarity 批量计算相似度
// 注意：此函数应该在 graphsearch 包中定义
// 已移动到 core 包中，此处保留注释作为参考
/*
func (g *graphsearch) batchComputeSimilarity(ctx context.Context, entities []graphsearch.Entity, queryEmbedding []float64, optimizer *VectorSearchOptimizer) []candidateResult {
	var candidates []candidateResult

	// 批量处理
	batchSize := 100
	if optimizer != nil && optimizer.batchSize > 0 {
		batchSize = optimizer.batchSize
	}

	for i := 0; i < len(entities); i += batchSize {
		end := i + batchSize
		if end > len(entities) {
			end = len(entities)
		}

		batch := entities[i:end]
		batchCandidates := g.computeBatchSimilarity(ctx, batch, queryEmbedding, optimizer)
		candidates = append(candidates, batchCandidates...)
	}

	return candidates
}
*/

// computeBatchSimilarity 计算批量相似度
// 注意：此函数应该在 graphsearch 包中定义
// 已移动到 core 包中，此处保留注释作为参考
/*
func (g *graphsearch) computeBatchSimilarity(ctx context.Context, entities []graphsearch.Entity, queryEmbedding []float64, optimizer *VectorSearchOptimizer) []candidateResult {
	candidates := make([]candidateResult, 0, len(entities))

	for _, entity := range entities {
		// 尝试从缓存获取
		var storedEmbedding []float64
		if optimizer != nil {
			optimizer.cacheMutex.RLock()
			if cached, ok := optimizer.cache[entity.EntityID]; ok {
				storedEmbedding = cached
				optimizer.cacheMutex.RUnlock()
			} else {
				optimizer.cacheMutex.RUnlock()
				// 解析 embedding
				if entity.Embedding != "" {
					if err := json.Unmarshal([]byte(entity.Embedding), &storedEmbedding); err == nil {
						// 缓存结果
						optimizer.cacheMutex.Lock()
						optimizer.cache[entity.EntityID] = storedEmbedding
						optimizer.cacheMutex.Unlock()
					}
				}
			}
		} else {
			// 不使用缓存，直接解析
			if entity.Embedding != "" {
				_ = json.Unmarshal([]byte(entity.Embedding), &storedEmbedding)
			}
		}

		if len(storedEmbedding) == 0 || len(storedEmbedding) != len(queryEmbedding) {
			continue
		}

		// 计算余弦相似度（使用优化的函数）
		similarity := optimizedCosineSimilarity(queryEmbedding, storedEmbedding)

		// 解析metadata
		var metadata map[string]any
		if entity.Metadata != "" {
			_ = json.Unmarshal([]byte(entity.Metadata), &metadata)
		}
		if metadata == nil {
			metadata = make(map[string]any)
		}

		candidates = append(candidates, candidateResult{
			entityID:   entity.EntityID,
			entityName: entity.EntityName,
			metadata:   metadata,
			similarity: similarity,
		})
	}

	return candidates
}
*/

// optimizedCosineSimilarity 优化的余弦相似度计算
func optimizedCosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	// 使用 SIMD 优化（如果可用）或循环展开
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ClearCache 清空缓存
func (o *VectorSearchOptimizer) ClearCache() {
	o.cacheMutex.Lock()
	defer o.cacheMutex.Unlock()
	o.cache = make(map[string][]float64)
}

// GetCacheSize 获取缓存大小
func (o *VectorSearchOptimizer) GetCacheSize() int {
	o.cacheMutex.RLock()
	defer o.cacheMutex.RUnlock()
	return len(o.cache)
}

// PreloadEmbeddings 预加载 embeddings 到缓存
// 注意：此函数应该在 graphsearch 包中定义
// 已移动到 core 包中，此处保留注释作为参考
/*
func (g *graphsearch) PreloadEmbeddings(ctx context.Context, optimizer *VectorSearchOptimizer, limit int) error {
	if !g.initialized {
		return fmt.Errorf("store not initialized, call Initialize first")
	}

	var entities []graphsearch.Entity
	query := g.db.WithContext(ctx).Table(g.tableName).
		Where("embedding IS NOT NULL AND embedding != '' AND embedding_status = ?", "completed")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&entities).Error; err != nil {
		return fmt.Errorf("failed to load entities: %w", err)
	}

	optimizer.cacheMutex.Lock()
	defer optimizer.cacheMutex.Unlock()

	for _, entity := range entities {
		if entity.Embedding != "" {
			var embedding []float64
			if err := json.Unmarshal([]byte(entity.Embedding), &embedding); err == nil {
				optimizer.cache[entity.EntityID] = embedding
			}
		}
	}

	return nil
}
*/
