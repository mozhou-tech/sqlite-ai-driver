package imagerag

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	lightrag "github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// ImageRAG 基于DuckDB的图片和文本RAG系统
type ImageRAG struct {
	db         lightrag.Database
	workingDir string
	embedder   lightrag.Embedder
	ocr        OCR

	// 集合
	images Collection
	texts  Collection

	// 搜索组件
	fulltext lightrag.FulltextSearch
	vector   lightrag.VectorSearch

	initialized bool
}

// Options ImageRAG配置选项
type Options struct {
	WorkingDir string
	Embedder   lightrag.Embedder
	OCR        OCR
}

// New 创建ImageRAG实例
func New(opts Options) *ImageRAG {
	return &ImageRAG{
		workingDir: opts.WorkingDir,
		embedder:   opts.Embedder,
		ocr:        opts.OCR,
	}
}

// InitializeStorages 初始化存储后端
func (r *ImageRAG) InitializeStorages(ctx context.Context) error {
	if r.initialized {
		return nil
	}

	if r.workingDir == "" {
		r.workingDir = "./imagerag_storage"
	}

	// 创建数据库
	db, err := lightrag.CreateDatabase(ctx, lightrag.DatabaseOptions{
		Name: "imagerag",
		Path: filepath.Join(r.workingDir, "imagerag.db"),
		GraphOptions: &lightrag.GraphOptions{
			Enabled: false, // ImageRAG不需要图数据库
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	r.db = db

	// 初始化图片集合
	imageSchema := lightrag.Schema{
		PrimaryKey: "id",
		RevField:   "_rev",
	}
	images, err := db.Collection(ctx, "images", imageSchema)
	if err != nil {
		return fmt.Errorf("failed to create images collection: %w", err)
	}
	r.images = images

	// 初始化文本集合
	textSchema := lightrag.Schema{
		PrimaryKey: "id",
		RevField:   "_rev",
	}
	texts, err := db.Collection(ctx, "texts", textSchema)
	if err != nil {
		return fmt.Errorf("failed to create texts collection: %w", err)
	}
	r.texts = texts

	// 使用 errgroup 并行初始化搜索索引
	g, _ := errgroup.WithContext(ctx)

	// 初始化图片全文搜索（基于OCR文本）
	g.Go(func() error {
		fulltext, err := lightrag.AddFulltextSearch(images, lightrag.FulltextSearchConfig{
			Identifier: "images_fulltext",
			DocToString: func(doc map[string]any) string {
				ocrText, _ := doc["ocr_text"].(string)
				return ocrText
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add images fulltext search: %w", err)
		}
		r.fulltext = fulltext
		return nil
	})

	// 初始化图片向量搜索
	if r.embedder != nil {
		g.Go(func() error {
			vector, err := lightrag.AddVectorSearch(images, lightrag.VectorSearchConfig{
				Identifier: "images_vector",
				DocToEmbedding: func(doc map[string]any) ([]float64, error) {
					// 优先使用OCR文本，如果没有则使用图片路径描述
					ocrText, _ := doc["ocr_text"].(string)
					if ocrText == "" {
						imagePath, _ := doc["image_path"].(string)
						ocrText = fmt.Sprintf("图片: %s", imagePath)
					}
					// 使用 context.Background() 避免 context canceled 错误
					return r.embedder.Embed(context.Background(), ocrText)
				},
				Dimensions: r.embedder.Dimensions(),
			})
			if err != nil {
				return fmt.Errorf("failed to add images vector search: %w", err)
			}
			r.vector = vector
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	r.initialized = true
	logrus.Info("ImageRAG storages initialized successfully")
	return nil
}

// InsertImage 插入图片
func (r *ImageRAG) InsertImage(ctx context.Context, imagePath string, metadata map[string]any) error {
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
	if metadata != nil {
		metadataJSON, err := json.Marshal(metadata)
		if err == nil {
			doc["metadata"] = string(metadataJSON)
		}
	}

	_, err = r.images.Insert(ctx, doc)
	if err != nil {
		return fmt.Errorf("failed to insert image: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"image_path":   imagePath,
		"ocr_text_len": len(ocrText),
	}).Info("Image inserted successfully")

	return nil
}

// InsertText 插入文本
func (r *ImageRAG) InsertText(ctx context.Context, text string, metadata map[string]any) error {
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
	if metadata != nil {
		metadataJSON, err := json.Marshal(metadata)
		if err == nil {
			doc["metadata"] = string(metadataJSON)
		}
	}

	_, err := r.texts.Insert(ctx, doc)
	if err != nil {
		return fmt.Errorf("failed to insert text: %w", err)
	}

	return nil
}

// Search 执行搜索
func (r *ImageRAG) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}

	if limit <= 0 {
		limit = 10
	}

	var results []SearchResult

	// 如果提供了embedder，执行向量搜索
	if r.embedder != nil && r.vector != nil {
		queryEmbedding, err := r.embedder.Embed(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to generate query embedding: %w", err)
		}

		vectorResults, err := r.vector.Search(ctx, queryEmbedding, lightrag.VectorSearchOptions{
			Limit: limit,
		})
		if err != nil {
			logrus.WithError(err).Warn("Vector search failed, falling back to fulltext search")
		} else {
			for _, result := range vectorResults {
				docData := result.Document.Data()
				results = append(results, SearchResult{
					ID:      result.Document.ID(),
					Content: getContentFromDoc(docData),
					Score:   result.Score,
					Source:  "vector",
					Data:    docData,
				})
			}
		}
	}

	// 如果向量搜索没有结果或失败，使用全文搜索
	if len(results) == 0 && r.fulltext != nil {
		fulltextResults, err := r.fulltext.FindWithScores(ctx, query, lightrag.FulltextSearchOptions{
			Limit: limit,
		})
		if err != nil {
			logrus.WithError(err).Warn("Fulltext search failed")
		} else {
			for _, result := range fulltextResults {
				docData := result.Document.Data()
				results = append(results, SearchResult{
					ID:      result.Document.ID(),
					Content: getContentFromDoc(docData),
					Score:   result.Score,
					Source:  "fulltext",
					Data:    docData,
				})
			}
		}
	}

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

// SearchResult 搜索结果
type SearchResult struct {
	ID      string
	Content string
	Score   float64
	Source  string // "vector" 或 "fulltext"
	Data    map[string]any
}

// Collection 集合接口（简化版，直接使用lightrag的Collection）
type Collection = lightrag.Collection

// Close 关闭ImageRAG
func (r *ImageRAG) Close(ctx context.Context) error {
	if r.db != nil {
		return r.db.Close(ctx)
	}
	return nil
}
