package imagesearch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	"github.com/sirupsen/logrus"
)

// ImageSearch 基于DuckDB的图片和文本RAG系统
type ImageSearch struct {
	db            *sql.DB
	workingDir    string
	textEmbedder  Embedder
	imageEmbedder Embedder
	ocr           OCR

	// 集合
	images *Collection
	texts  *Collection

	// 搜索组件
	imagesVector *VectorSearch
	textsVector  *VectorSearch

	initialized bool
	mu          sync.Mutex
}

// New 创建ImageSearch实例
func New(opts Options) *ImageSearch {
	return &ImageSearch{
		workingDir:    opts.WorkingDir,
		textEmbedder:  opts.TextEmbedder,
		imageEmbedder: opts.ImageEmbedder,
		ocr:           opts.OCR,
	}
}

// InitializeStorages 初始化存储后端
func (r *ImageSearch) InitializeStorages(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.initialized {
		return nil
	}

	// 打开DuckDB数据库
	// 注意：无论传入什么路径，都会被 duckdb-driver 统一映射到共享数据库文件 {DATA_DIR}/indexing/all.db
	// 目录创建由 duckdb-driver 自动处理，无需在此处创建
	// 使用简单的路径标识即可，实际路径会被映射到共享数据库
	db, err := sql.Open("duckdb", "imagesearch.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	r.db = db

	// 初始化图片集合
	images, err := r.createCollection(ctx, "images")
	if err != nil {
		return fmt.Errorf("failed to create images collection: %w", err)
	}
	r.images = images

	// 初始化文本集合
	texts, err := r.createCollection(ctx, "texts")
	if err != nil {
		return fmt.Errorf("failed to create texts collection: %w", err)
	}
	r.texts = texts

	// 初始化图片向量搜索（使用 image_embedding 字段）
	if r.imageEmbedder != nil {
		vector, err := r.addVectorSearch(images, "image_embedding", func(doc map[string]any) ([]float64, error) {
			// 优先使用OCR文本，如果没有则使用图片路径描述
			ocrText, _ := doc["ocr_text"].(string)
			if ocrText == "" {
				imagePath, _ := doc["image_path"].(string)
				ocrText = fmt.Sprintf("图片: %s", imagePath)
			}
			// 使用 context.Background() 避免 context canceled 错误
			return r.imageEmbedder.Embed(context.Background(), ocrText)
		})
		if err != nil {
			return fmt.Errorf("failed to add images vector search: %w", err)
		}
		r.imagesVector = vector
	}

	// 初始化文本向量搜索（使用 text_embedding 字段）
	if r.textEmbedder != nil {
		vector, err := r.addVectorSearch(texts, "text_embedding", func(doc map[string]any) ([]float64, error) {
			// 使用文本内容生成embedding
			content, _ := doc["content"].(string)
			if content == "" {
				return nil, fmt.Errorf("empty content")
			}
			// 使用 context.Background() 避免 context canceled 错误
			return r.textEmbedder.Embed(context.Background(), content)
		})
		if err != nil {
			return fmt.Errorf("failed to add texts vector search: %w", err)
		}
		r.textsVector = vector
	}

	r.initialized = true
	logrus.Info("ImageSearch storages initialized successfully")
	return nil
}

// InsertImage 插入图片
func (r *ImageSearch) InsertImage(ctx context.Context, imagePath string, metadata map[string]any) error {
	if !r.initialized {
		return fmt.Errorf("storages not initialized")
	}

	// 获取图片信息
	imageInfo, err := GetImageInfo(imagePath)
	if err != nil {
		return fmt.Errorf("failed to get image info: %w", err)
	}

	// 提取OCR文本
	var ocrText string
	if r.ocr != nil {
		ocrText, err = r.ocr.ExtractText(ctx, imagePath)
		if err != nil {
			logrus.WithError(err).WithField("image_path", imagePath).Warn("Failed to extract OCR text, continuing without it")
			// 继续处理，即使OCR失败
		}
	}

	// 构建文档
	doc := map[string]any{
		"id":         fmt.Sprintf("%d", time.Now().UnixNano()),
		"image_path": imagePath,
		"ocr_text":   ocrText,
		"width":      imageInfo.Width,
		"height":     imageInfo.Height,
		"format":     imageInfo.Format,
		"size":       imageInfo.Size,
		"created_at": time.Now().Unix(),
	}

	// 合并元数据
	if metadata != nil {
		for k, v := range metadata {
			doc[k] = v
		}
	}

	// 将metadata字段序列化为JSON
	var metadataJSON string
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err == nil {
			metadataJSON = string(metadataBytes)
		}
	}

	// 插入到数据库
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (id, content, metadata, _rev, embedding_status)
		VALUES (?, ?, ?, 1, 'pending')
	`, r.images.tableName)

	// 将doc序列化为content字段（存储所有数据）
	contentJSON, _ := json.Marshal(doc)

	_, err = r.db.ExecContext(ctx, insertSQL, doc["id"], string(contentJSON), metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to insert image: %w", err)
	}

	// 如果提供了imageEmbedder，启动后台embedding处理
	if r.imageEmbedder != nil && r.imagesVector != nil {
		r.imagesVector.processPendingEmbeddings(ctx)
	}

	logrus.WithFields(logrus.Fields{
		"image_path":   imagePath,
		"ocr_text_len": len(ocrText),
	}).Info("Image inserted successfully")

	return nil
}

// InsertText 插入文本
func (r *ImageSearch) InsertText(ctx context.Context, text string, metadata map[string]any) error {
	if !r.initialized {
		return fmt.Errorf("storages not initialized")
	}

	// 如果文本太短，跳过
	if len([]rune(text)) <= 10 {
		logrus.WithFields(logrus.Fields{
			"content_len": len([]rune(text)),
		}).Debug("Skipping text that is too short (<=10 characters)")
		return nil
	}

	doc := map[string]any{
		"id":         fmt.Sprintf("%d", time.Now().UnixNano()),
		"content":    text,
		"created_at": time.Now().Unix(),
	}

	// 合并元数据
	if metadata != nil {
		for k, v := range metadata {
			doc[k] = v
		}
	}

	// 将metadata字段序列化为JSON
	var metadataJSON string
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err == nil {
			metadataJSON = string(metadataBytes)
		}
	}

	// 插入到数据库
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (id, content, metadata, _rev, embedding_status)
		VALUES (?, ?, ?, 1, 'pending')
	`, r.texts.tableName)

	contentJSON, _ := json.Marshal(doc)

	_, err := r.db.ExecContext(ctx, insertSQL, doc["id"], string(contentJSON), metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to insert text: %w", err)
	}

	// 如果提供了textEmbedder，启动后台embedding处理
	if r.textEmbedder != nil && r.textsVector != nil {
		r.textsVector.processPendingEmbeddings(ctx)
	}

	return nil
}

// Search 执行搜索
func (r *ImageSearch) Search(ctx context.Context, query string, limit int, metadataFilter MetadataFilter) ([]SearchResult, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}

	if limit <= 0 {
		limit = 10
	}

	var results []SearchResult

	// 执行向量搜索
	// 优先使用textEmbedder进行查询向量化（因为查询通常是文本）
	if r.textEmbedder != nil {
		queryEmbedding, err := r.textEmbedder.Embed(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to generate query embedding: %w", err)
		}

		// 搜索图片集合（使用textEmbedder生成的查询向量，因为图片的embedding也是基于OCR文本生成的）
		if r.imagesVector != nil {
			vectorResults, err := r.imagesVector.Search(ctx, queryEmbedding, limit, metadataFilter)
			if err != nil {
				return nil, fmt.Errorf("images vector search failed: %w", err)
			}
			results = append(results, vectorResults...)
		}

		// 搜索文本集合
		if r.textsVector != nil {
			vectorResults, err := r.textsVector.Search(ctx, queryEmbedding, limit, metadataFilter)
			if err != nil {
				return nil, fmt.Errorf("texts vector search failed: %w", err)
			}
			results = append(results, vectorResults...)
		}
	} else if r.imageEmbedder != nil {
		// 如果没有textEmbedder，尝试使用imageEmbedder
		queryEmbedding, err := r.imageEmbedder.Embed(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to generate query embedding: %w", err)
		}

		if r.imagesVector != nil {
			vectorResults, err := r.imagesVector.Search(ctx, queryEmbedding, limit, metadataFilter)
			if err != nil {
				return nil, fmt.Errorf("images vector search failed: %w", err)
			}
			results = append(results, vectorResults...)
		}
	} else {
		return nil, fmt.Errorf("no embedder available for search")
	}

	// 限制结果数量
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// HybridSearch 执行文本和图像混合搜索，权重分别为0.5
func (r *ImageSearch) HybridSearch(ctx context.Context, query string, limit int, metadataFilter MetadataFilter) ([]SearchResult, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}

	if limit <= 0 {
		limit = 10
	}

	// 检查是否有必要的embedder
	if r.textEmbedder == nil {
		return nil, fmt.Errorf("text embedder is required for hybrid search")
	}
	if r.imageEmbedder == nil {
		return nil, fmt.Errorf("image embedder is required for hybrid search")
	}

	// 权重配置
	textWeight := 0.5
	imageWeight := 0.5

	// 使用文本embedder生成查询向量，用于文本搜索
	textQueryEmbedding, err := r.textEmbedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate text query embedding: %w", err)
	}

	// 使用图像embedder生成查询向量，用于图像搜索
	imageQueryEmbedding, err := r.imageEmbedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate image query embedding: %w", err)
	}

	var allResults []SearchResult

	// 搜索文本集合（使用文本embedder）
	if r.textsVector != nil {
		textResults, err := r.textsVector.Search(ctx, textQueryEmbedding, limit, metadataFilter)
		if err != nil {
			return nil, fmt.Errorf("texts vector search failed: %w", err)
		}
		// 应用文本权重
		for i := range textResults {
			textResults[i].Score *= textWeight
			textResults[i].Source = "text"
		}
		allResults = append(allResults, textResults...)
	}

	// 搜索图像集合（使用图像embedder）
	if r.imagesVector != nil {
		imageResults, err := r.imagesVector.Search(ctx, imageQueryEmbedding, limit, metadataFilter)
		if err != nil {
			return nil, fmt.Errorf("images vector search failed: %w", err)
		}
		// 应用图像权重
		for i := range imageResults {
			imageResults[i].Score *= imageWeight
			imageResults[i].Source = "image"
		}
		allResults = append(allResults, imageResults...)
	}

	// 合并和去重结果（基于ID）
	// 如果同一个ID在文本和图像结果中都出现，合并分数（相加）
	resultMap := make(map[string]*SearchResult)
	for i := range allResults {
		result := &allResults[i]
		if existing, exists := resultMap[result.ID]; exists {
			// 如果已存在，合并分数（相加，因为每个分数都已经乘以了对应的权重）
			existing.Score += result.Score
			// 如果来源不同，更新为混合来源
			if existing.Source != result.Source {
				existing.Source = "hybrid"
			}
		} else {
			// 创建新的结果副本
			newResult := *result
			resultMap[result.ID] = &newResult
		}
	}

	// 转换为切片并按分数排序
	results := make([]SearchResult, 0, len(resultMap))
	for _, result := range resultMap {
		results = append(results, *result)
	}

	// 按分数降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// 限制结果数量
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// getContentFromDoc 从文档中提取内容
func getContentFromDoc(doc map[string]any) string {
	// 优先返回OCR文本
	if ocrText, ok := doc["ocr_text"].(string); ok && ocrText != "" {
		return ocrText
	}
	// 其次返回content字段
	if content, ok := doc["content"].(string); ok {
		return content
	}
	// 最后返回图片路径
	if imagePath, ok := doc["image_path"].(string); ok {
		return imagePath
	}
	return ""
}

// Close 关闭ImageSearch
func (r *ImageSearch) Close(ctx context.Context) error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}
