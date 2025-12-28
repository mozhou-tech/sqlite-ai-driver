package lightrag

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// LightRAG 基于新的driver实现的 LightRAG
type LightRAG struct {
	db         Database
	workingDir string
	embedder   Embedder
	llm        LLM

	// 集合
	docs Collection

	// 搜索组件
	fulltext FulltextSearch
	vector   VectorSearch
	graph    GraphDatabase

	initialized bool
	wg          sync.WaitGroup
	llmSem      chan struct{} // 用于限制 LLM 并发
}

// Options LightRAG 配置选项
type Options struct {
	WorkingDir       string
	Embedder         Embedder
	LLM              LLM
	MaxConcurrentLLM int // 最大并发 LLM 请求数，默认为 10
}

// New 创建 LightRAG 实例
func New(opts Options) *LightRAG {
	if opts.MaxConcurrentLLM <= 0 {
		opts.MaxConcurrentLLM = 10
	}
	return &LightRAG{
		workingDir: opts.WorkingDir,
		embedder:   opts.Embedder,
		llm:        opts.LLM,
		llmSem:     make(chan struct{}, opts.MaxConcurrentLLM),
	}
}

// InitializeStorages 初始化存储后端
func (r *LightRAG) InitializeStorages(ctx context.Context) error {
	if r.initialized {
		return nil
	}

	if r.workingDir == "" {
		r.workingDir = "./rag_storage"
	}

	// 创建数据库
	db, err := CreateDatabase(ctx, DatabaseOptions{
		Name: "lightrag",
		Path: filepath.Join(r.workingDir, "lightrag.db"),
		GraphOptions: &GraphOptions{
			Enabled: true,
			Backend: "cayley",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	r.db = db
	r.graph = db.Graph()

	// 初始化文档集合
	docSchema := Schema{
		PrimaryKey: "id",
		RevField:   "_rev",
	}
	docs, err := db.Collection(ctx, "documents", docSchema)
	if err != nil {
		return fmt.Errorf("failed to create documents collection: %w", err)
	}
	r.docs = docs

	// 使用 errgroup 并行初始化搜索索引
	g, gCtx := errgroup.WithContext(ctx)

	// 初始化全文搜索
	g.Go(func() error {
		fulltext, err := AddFulltextSearch(docs, FulltextSearchConfig{
			Identifier: "docs_fulltext",
			DocToString: func(doc map[string]any) string {
				content, _ := doc["content"].(string)
				return content
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add fulltext search: %w", err)
		}
		r.fulltext = fulltext
		return nil
	})

	// 初始化向量搜索
	if r.embedder != nil {
		g.Go(func() error {
			vector, err := AddVectorSearch(docs, VectorSearchConfig{
				Identifier: "docs_vector",
				DocToEmbedding: func(doc map[string]any) ([]float64, error) {
					content, _ := doc["content"].(string)
					return r.embedder.Embed(gCtx, content)
				},
				Dimensions: r.embedder.Dimensions(),
			})
			if err != nil {
				return fmt.Errorf("failed to add vector search: %w", err)
			}
			r.vector = vector
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	r.initialized = true
	logrus.Info("LightRAG storages initialized successfully")
	return nil
}

// Insert 插入文本
func (r *LightRAG) Insert(ctx context.Context, text string) error {
	if !r.initialized {
		return fmt.Errorf("storages not initialized")
	}

	doc := map[string]any{
		"id":         fmt.Sprintf("%d", time.Now().UnixNano()),
		"content":    text,
		"created_at": time.Now().Unix(),
	}

	_, err := r.docs.Insert(ctx, doc)
	if err != nil {
		return fmt.Errorf("failed to insert document: %w", err)
	}

	// 提取并存储实体与关系
	if r.llm != nil && r.graph != nil {
		docID := doc["id"].(string)
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()

			// 获取信号量
			select {
			case r.llmSem <- struct{}{}:
				defer func() { <-r.llmSem }()
			case <-ctx.Done():
				return
			}

			// 在后台执行提取，避免阻塞主流程
			err := r.extractAndStore(context.Background(), text, docID)
			if err != nil {
				logrus.WithError(err).Error("Failed to extract and store graph data")
			}
		}()
	}

	return nil
}

// ListDocuments 获取文档列表
func (r *LightRAG) ListDocuments(ctx context.Context, limit, offset int) ([]map[string]any, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}

	docs, err := r.docs.Find(ctx, FindOptions{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		results = append(results, doc.Data())
	}

	return results, nil
}

// DeleteDocument 删除文档
func (r *LightRAG) DeleteDocument(ctx context.Context, id string) error {
	if !r.initialized {
		return fmt.Errorf("storages not initialized")
	}

	return r.docs.Delete(ctx, id)
}

func (r *LightRAG) extractQueryEntities(ctx context.Context, query string) ([]string, error) {
	if r.llm == nil {
		return nil, nil
	}

	promptStr, err := GetQueryEntityPrompt(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get query entity prompt: %w", err)
	}
	response, err := r.llm.Complete(ctx, promptStr)
	if err != nil {
		return nil, err
	}

	logrus.WithField("raw_response", response).Debug("LLM response for query entities")

	jsonStr := response
	idxStart := strings.Index(jsonStr, "[")
	idxEnd := strings.LastIndex(jsonStr, "]")
	if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
		return nil, fmt.Errorf("no JSON array found in response: %s", response)
	}
	jsonStr = jsonStr[idxStart : idxEnd+1]

	var entities []string
	if err := json.Unmarshal([]byte(jsonStr), &entities); err != nil {
		logrus.WithField("jsonStr", jsonStr).Error("Failed to parse query entities")
		return nil, fmt.Errorf("failed to parse query entities: %w", err)
	}

	return entities, nil
}

func (r *LightRAG) extractAndStore(ctx context.Context, text string, docID string) error {
	promptStr, err := GetExtractionPrompt(ctx, text)
	if err != nil {
		return fmt.Errorf("failed to get extraction prompt: %w", err)
	}
	response, err := r.llm.Complete(ctx, promptStr)
	if err != nil {
		return err
	}

	// 尝试解析 JSON，增强健壮性
	jsonStr := response
	idxStart := strings.Index(jsonStr, "{")
	idxEnd := strings.LastIndex(jsonStr, "}")
	if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
		// 尝试检查是否是纯数组（有些 LLM 可能只返回数组）
		idxStart = strings.Index(jsonStr, "[")
		idxEnd = strings.LastIndex(jsonStr, "]")
		if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
			return fmt.Errorf("no JSON object or array found in response: %s", response)
		}
	}
	jsonStr = jsonStr[idxStart : idxEnd+1]

	var result ExtractionResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return fmt.Errorf("failed to parse extraction result: %w, response: %s", err, response)
	}

	logrus.WithFields(logrus.Fields{
		"doc_id":              docID,
		"entities_count":      len(result.Entities),
		"relationships_count": len(result.Relationships),
	}).Info("Extracted graph data from document")

	// 批量存储实体链接和关系（如果 driver 支持批量操作，这里可以进一步优化）
	// 目前 driver 接口是单条操作
	for _, entity := range result.Entities {
		if entity.Name == "" {
			continue
		}
		// 链接实体到文档
		err := r.graph.Link(ctx, entity.Name, "APPEARS_IN", docID)
		if err != nil {
			logrus.WithError(err).Errorf("Failed to link entity %s to doc %s", entity.Name, docID)
		}

		// 存储实体类型和描述
		if entity.Type != "" {
			_ = r.graph.Link(ctx, entity.Name, "TYPE", entity.Type)
		}
		if entity.Description != "" {
			_ = r.graph.Link(ctx, entity.Name, "DESCRIPTION", entity.Description)
		}
	}

	// 存储关系
	for _, rel := range result.Relationships {
		if rel.Source == "" || rel.Target == "" {
			continue
		}
		err := r.graph.Link(ctx, rel.Source, rel.Relation, rel.Target)
		if err != nil {
			logrus.WithError(err).Errorf("Failed to link nodes: %s -[%s]-> %s", rel.Source, rel.Relation, rel.Target)
		}
	}

	return nil
}

// InsertBatch 批量插入带元数据的文档
func (r *LightRAG) InsertBatch(ctx context.Context, documents []map[string]any) ([]string, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}

	for i := range documents {
		if id, ok := documents[i]["id"]; !ok || id == "" {
			documents[i]["id"] = fmt.Sprintf("%d-%d", time.Now().UnixNano(), i)
		}
		if _, ok := documents[i]["content"]; !ok {
			return nil, fmt.Errorf("document at index %d missing 'content' field", i)
		}
		if _, ok := documents[i]["created_at"]; !ok {
			documents[i]["created_at"] = time.Now().Unix()
		}
	}

	res, err := r.docs.BulkUpsert(ctx, documents)
	if err != nil {
		return nil, fmt.Errorf("failed to bulk insert documents: %w", err)
	}

	ids := make([]string, 0, len(res))
	for _, doc := range res {
		ids = append(ids, doc.ID())
		// 批量插入时也进行图谱提取，使用信号量控制并发
		if r.llm != nil && r.graph != nil {
			content, _ := doc.Data()["content"].(string)
			docID := doc.ID()
			r.wg.Add(1)
			go func(c string, id string) {
				defer r.wg.Done()

				// 获取信号量
				select {
				case r.llmSem <- struct{}{}:
					defer func() { <-r.llmSem }()
				case <-ctx.Done():
					return
				}

				r.extractAndStore(context.Background(), c, id)
			}(content, docID)
		}
	}

	return ids, nil
}

// Query 执行查询
func (r *LightRAG) Query(ctx context.Context, query string, param QueryParam) (string, error) {
	results, err := r.Retrieve(ctx, query, param)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No relevant information found.", nil
	}

	// 简单的上下文拼接
	contextText := ""

	// 首先添加知识图谱信息（如果存在）
	uniqueTriples := make(map[string]bool)
	var graphLines []string
	for _, res := range results {
		for _, triple := range res.RecalledTriples {
			key := fmt.Sprintf("%s-%s-%s", triple.Source, triple.Relation, triple.Target)
			if !uniqueTriples[key] {
				uniqueTriples[key] = true
				graphLines = append(graphLines, fmt.Sprintf("- %s -[%s]-> %s", triple.Source, triple.Relation, triple.Target))
			}
		}
	}

	if len(graphLines) > 0 {
		contextText += "Knowledge Graph recalled:\n"
		contextText += strings.Join(graphLines, "\n")
		contextText += "\n\n"
	}

	contextText += "Relevant Documents:\n"
	for i, res := range results {
		contextText += fmt.Sprintf("[%d] %s\n", i+1, res.Content)
	}

	if r.llm != nil {
		promptStr, err := GetRAGAnswerPrompt(ctx, contextText, query)
		if err != nil {
			return "", fmt.Errorf("failed to get RAG answer prompt: %w", err)
		}
		return r.llm.Complete(ctx, promptStr)
	}

	return contextText, nil
}

// Retrieve 执行检索
func (r *LightRAG) Retrieve(ctx context.Context, query string, param QueryParam) ([]SearchResult, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}

	if param.Limit <= 0 {
		param.Limit = 5
	}

	var rawResults []FulltextSearchResult
	var recalledTriples []Relationship
	var err error

	switch param.Mode {
	case ModeVector, ModeNaive:
		if r.vector == nil {
			return nil, fmt.Errorf("vector search not available")
		}
		emb, err := r.embedder.Embed(ctx, query)
		if err != nil {
			return nil, err
		}
		vecResults, err := r.vector.Search(ctx, emb, VectorSearchOptions{
			Limit:    param.Limit,
			Selector: param.Filters,
		})
		if err != nil {
			logrus.WithError(err).Warn("Vector search failed")
			return nil, err
		}
		logrus.WithField("count", len(vecResults)).Debug("Vector search returned results")
		for _, v := range vecResults {
			rawResults = append(rawResults, FulltextSearchResult{
				Document: v.Document,
				Score:    v.Score,
			})
		}
	case ModeFulltext:
		rawResults, err = r.fulltext.FindWithScores(ctx, query, FulltextSearchOptions{
			Limit:    param.Limit,
			Selector: param.Filters,
		})
		if err != nil {
			logrus.WithError(err).Warn("Fulltext search failed")
			return nil, err
		}
		logrus.WithField("count", len(rawResults)).Debug("Fulltext search returned results")
	case ModeLocal:
		if r.graph == nil {
			return nil, fmt.Errorf("graph search not available")
		}
		entities, err := r.extractQueryEntities(ctx, query)
		if err != nil {
			logrus.WithError(err).Warn("Failed to extract query entities, falling back to fulltext")
			return r.Retrieve(ctx, query, QueryParam{Mode: ModeFulltext, Limit: param.Limit})
		}

		logrus.WithFields(logrus.Fields{
			"query":    query,
			"entities": entities,
		}).Info("Extracted entities for local search")

		docIDMap := make(map[string]bool)
		var mu sync.Mutex
		g, gCtx := errgroup.WithContext(ctx)

		for _, entity := range entities {
			e := entity
			g.Go(func() error {
				neighbors, _ := r.graph.GetNeighbors(gCtx, e, "APPEARS_IN")
				if len(neighbors) > 0 {
					mu.Lock()
					for _, id := range neighbors {
						docIDMap[id] = true
						logrus.Infof("Local Recalled: Entity %s is in Document %s", e, id)
					}
					mu.Unlock()
				}
				// 也考虑一度邻居关联的文档
				related, _ := r.graph.GetNeighbors(gCtx, e, "")
				for _, relNode := range related {
					if relNode == e {
						continue
					}
					// 查找关系
					res, _ := r.graph.Query().V(e).Both().All(gCtx)
					mu.Lock()
					for _, qr := range res {
						if qr.Predicate != "APPEARS_IN" && (qr.Subject == relNode || qr.Object == relNode) {
							logrus.Infof("Local Recalled: %s -[%s]-> %s", qr.Subject, qr.Predicate, qr.Object)
							recalledTriples = append(recalledTriples, Relationship{
								Source:   qr.Subject,
								Target:   qr.Object,
								Relation: qr.Predicate,
							})
						}
					}
					mu.Unlock()

					docNeighbors, _ := r.graph.GetNeighbors(gCtx, relNode, "APPEARS_IN")
					if len(docNeighbors) > 0 {
						mu.Lock()
						for _, id := range docNeighbors {
							docIDMap[id] = true
							logrus.Infof("Local Recalled: Entity %s is related to %s which is in Document %s", e, relNode, id)
						}
						mu.Unlock()
					}
				}
				return nil
			})
		}
		_ = g.Wait()

		logrus.WithField("total_unique_docs", len(docIDMap)).Info("Graph traversal completed")

		// 并行获取文档内容
		type scoredDoc struct {
			doc   Document
			score float64
		}
		scoredDocs := make(chan scoredDoc, len(docIDMap))
		g, gCtx = errgroup.WithContext(ctx)

		limit := param.Limit
		count := 0
		for id := range docIDMap {
			if count >= limit*2 { // 限制获取数量，避免过多
				break
			}
			docID := id
			g.Go(func() error {
				doc, err := r.docs.FindByID(gCtx, docID)
				if err == nil && doc != nil {
					// 应用过滤器
					if matchesFilters(doc.Data(), param.Filters) {
						scoredDocs <- scoredDoc{doc: doc, score: 1.0}
					}
				}
				return nil
			})
			count++
		}

		go func() {
			g.Wait()
			close(scoredDocs)
		}()

		for sd := range scoredDocs {
			rawResults = append(rawResults, FulltextSearchResult{
				Document: sd.doc,
				Score:    sd.score,
			})
			if len(rawResults) >= param.Limit {
				break
			}
		}

	case ModeGraph:
		if r.graph == nil {
			return nil, fmt.Errorf("graph search not available")
		}

		// Graph 模式：纯知识图谱查询，不使用向量或全文搜索
		// 1. 获取图谱数据
		// 确保 context 中带有 ModeGraph，以便 SearchGraphWithDepth 知道要跳过向量扩展
		gCtx := context.WithValue(ctx, "rag_mode", ModeGraph)
		graphData, err := r.SearchGraphWithDepth(gCtx, query, 2) // 使用深度 2 召回更多关联
		if err != nil {
			return nil, fmt.Errorf("graph search failed: %w", err)
		}

		recalledTriples = graphData.Relationships

		// 2. 根据召回的实体找到关联的文档
		docIDMap := make(map[string]bool)
		for _, entity := range graphData.Entities {
			neighbors, _ := r.graph.GetNeighbors(ctx, entity.Name, "APPEARS_IN")
			for _, id := range neighbors {
				docIDMap[id] = true
			}
		}

		// 3. 获取文档内容
		g, gCtx := errgroup.WithContext(ctx)
		type scoredDoc struct {
			doc   Document
			score float64
		}
		scoredDocs := make(chan scoredDoc, len(docIDMap))

		count := 0
		for id := range docIDMap {
			if count >= param.Limit*2 {
				break
			}
			docID := id
			g.Go(func() error {
				doc, err := r.docs.FindByID(gCtx, docID)
				if err == nil && doc != nil {
					if matchesFilters(doc.Data(), param.Filters) {
						scoredDocs <- scoredDoc{doc: doc, score: 1.0}
					}
				}
				return nil
			})
			count++
		}

		go func() {
			_ = g.Wait()
			close(scoredDocs)
		}()

		for sd := range scoredDocs {
			rawResults = append(rawResults, FulltextSearchResult{
				Document: sd.doc,
				Score:    sd.score,
			})
			if len(rawResults) >= param.Limit {
				break
			}
		}
	case ModeGlobal:
		// 全局搜索：结合图谱搜索和混合搜索
		graphData, _ := r.SearchGraph(ctx, query)
		if graphData != nil {
			recalledTriples = graphData.Relationships
		}
		// 获取混合搜索结果
		hybridResults, err := r.Retrieve(ctx, query, QueryParam{Mode: ModeHybrid, Limit: param.Limit})
		if err == nil {
			// 将图谱三元组注入到每个结果中，以便上层获取
			for i := range hybridResults {
				hybridResults[i].RecalledTriples = recalledTriples
			}
			return hybridResults, nil
		}
		return nil, err
	case ModeHybrid:
		// 实现真正的混合搜索（向量 + 全文 + 知识图谱）
		var ftResults []FulltextSearchResult
		var vecResults []VectorSearchResult
		var mu sync.Mutex

		g, gCtx := errgroup.WithContext(ctx)

		// 1. 全文搜索
		g.Go(func() error {
			var err error
			ftResults, err = r.fulltext.FindWithScores(gCtx, query, FulltextSearchOptions{
				Limit:    param.Limit * 2,
				Selector: param.Filters,
			})
			if err != nil {
				logrus.WithError(err).Error("Fulltext search failed in hybrid mode")
				ftResults = []FulltextSearchResult{}
			}
			return nil
		})

		// 2. 向量搜索
		if r.vector != nil && r.embedder != nil {
			g.Go(func() error {
				emb, err := r.embedder.Embed(gCtx, query)
				if err != nil {
					logrus.WithError(err).Error("Embedding failed in hybrid mode")
					return nil
				}
				vecResults, err = r.vector.Search(gCtx, emb, VectorSearchOptions{
					Limit:    param.Limit * 2,
					Selector: param.Filters,
				})
				if err != nil {
					logrus.WithError(err).Error("Vector search failed in hybrid mode")
					vecResults = []VectorSearchResult{}
				}
				return nil
			})
		}

		// 3. 知识图谱搜索
		if r.graph != nil {
			g.Go(func() error {
				graphData, err := r.SearchGraph(gCtx, query)
				if err == nil && graphData != nil {
					mu.Lock()
					recalledTriples = append(recalledTriples, graphData.Relationships...)
					mu.Unlock()
				}
				return nil
			})
		}

		_ = g.Wait()

		logrus.WithFields(logrus.Fields{
			"ft_count":    len(ftResults),
			"vec_count":   len(vecResults),
			"graph_count": len(recalledTriples),
		}).Debug("Hybrid search recall completed")

		// 使用简单的 RRF 融合或加权融合
		docScores := make(map[string]float64)
		docMap := make(map[string]Document)

		for i, res := range ftResults {
			score := 1.0 / float64(i+60) // RRF
			docScores[res.Document.ID()] += score
			docMap[res.Document.ID()] = res.Document
		}

		for i, res := range vecResults {
			score := 1.0 / float64(i+60) // RRF
			docScores[res.Document.ID()] += score
			docMap[res.Document.ID()] = res.Document
		}

		// 如果两种搜索都没有结果，回退到纯全文搜索
		if len(docScores) == 0 {
			// 回退到纯全文搜索，使用更大的 limit
			fallbackResults, err := r.fulltext.FindWithScores(ctx, query, FulltextSearchOptions{
				Limit:    param.Limit,
				Selector: param.Filters,
			})
			if err == nil && len(fallbackResults) > 0 {
				for _, res := range fallbackResults {
					rawResults = append(rawResults, res)
				}
			}
		} else {
			// 排序并取 Top N
			for id, score := range docScores {
				rawResults = append(rawResults, FulltextSearchResult{
					Document: docMap[id],
					Score:    score,
				})
			}
			// 按分数降序排序
			sort.Slice(rawResults, func(i, j int) bool {
				return rawResults[i].Score > rawResults[j].Score
			})

			if len(rawResults) > param.Limit {
				rawResults = rawResults[:param.Limit]
			}

			// 归一化处理：使最高分为 1.0
			if len(rawResults) > 0 {
				maxScore := rawResults[0].Score
				if maxScore > 0 {
					for i := range rawResults {
						rawResults[i].Score = rawResults[i].Score / maxScore
					}
				}
			}
		}
	default:
		rawResults, err = r.fulltext.FindWithScores(ctx, query, FulltextSearchOptions{Limit: param.Limit})
		if err != nil {
			return nil, err
		}
	}

	results := make([]SearchResult, 0, len(rawResults))
	for _, res := range rawResults {
		content, _ := res.Document.Data()["content"].(string)
		results = append(results, SearchResult{
			ID:              res.Document.ID(),
			Content:         content,
			Score:           res.Score,
			Metadata:        res.Document.Data(),
			RecalledTriples: recalledTriples,
		})
	}

	// 打印召回文档的评分
	for i, res := range results {
		logrus.WithFields(logrus.Fields{
			"index": i + 1,
			"id":    res.ID,
			"score": res.Score,
			"mode":  param.Mode,
		}).Info("LightRAG recalled document")
	}

	return results, nil
}

// ExportFullGraph 导出完整的知识图谱
func (r *LightRAG) ExportFullGraph(ctx context.Context) (*GraphData, error) {
	return r.ExportGraph(ctx, "")
}

// ExportGraph 导出知识图谱，可选指定文档 ID 过滤
func (r *LightRAG) ExportGraph(ctx context.Context, docID string) (*GraphData, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}
	if r.graph == nil {
		return nil, fmt.Errorf("graph database not available")
	}

	triples, err := r.graph.AllTriples(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all triples: %w", err)
	}

	// 如果指定了文档过滤，先找到该文档包含的所有实体
	allowedEntities := make(map[string]bool)
	if docID != "" {
		for _, t := range triples {
			if t.Predicate == "APPEARS_IN" && t.Object == docID {
				allowedEntities[t.Subject] = true
			}
		}
	}

	result := &GraphData{
		Entities:      make([]Entity, 0),
		Relationships: make([]Relationship, 0),
	}

	entityMap := make(map[string]*Entity)

	// 第一遍：识别所有实体并处理特殊谓词
	for _, t := range triples {
		// 忽略指向文档的链接
		if t.Predicate == "APPEARS_IN" {
			continue
		}

		if docID != "" && !allowedEntities[t.Subject] {
			continue
		}

		// 收集实体信息
		if t.Predicate == "TYPE" || t.Predicate == "DESCRIPTION" {
			e, ok := entityMap[t.Subject]
			if !ok {
				e = &Entity{Name: t.Subject}
				entityMap[t.Subject] = e
			}
			if t.Predicate == "TYPE" {
				e.Type = t.Object
			} else {
				e.Description = t.Object
			}
			continue
		}

		// 记录普通关系
		// 如果指定了文档过滤，只有当源和目标实体都在该文档中时才记录（或者至少源在）
		if docID != "" && (!allowedEntities[t.Subject] || !allowedEntities[t.Object]) {
			continue
		}

		result.Relationships = append(result.Relationships, Relationship{
			Source:   t.Subject,
			Target:   t.Object,
			Relation: t.Predicate,
		})

		// 确保实体存在于 map 中
		if _, ok := entityMap[t.Subject]; !ok {
			entityMap[t.Subject] = &Entity{Name: t.Subject}
		}
		if _, ok := entityMap[t.Object]; !ok {
			entityMap[t.Object] = &Entity{Name: t.Object}
		}
	}

	// 转换 map 到 slice
	for _, e := range entityMap {
		result.Entities = append(result.Entities, *e)
	}

	return result, nil
}

// SearchGraph 仅从图谱检索实体和关系
func (r *LightRAG) SearchGraph(ctx context.Context, query string) (*GraphData, error) {
	return r.SearchGraphWithDepth(ctx, query, 1)
}

// SearchGraphWithDepth 从图谱检索实体和关系，支持指定搜索深度
func (r *LightRAG) SearchGraphWithDepth(ctx context.Context, query string, depth int) (*GraphData, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}
	if r.graph == nil {
		return nil, fmt.Errorf("graph database not available")
	}

	entities, err := r.extractQueryEntities(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to extract entities from query: %w", err)
	}

	result := &GraphData{
		Entities:      make([]Entity, 0),
		Relationships: make([]Relationship, 0),
	}

	entityMap := make(map[string]bool)
	relMap := make(map[string]bool)

	// 1. 扩展实体：如果提取的实体在图中找不到直接关系，尝试通过语义搜索寻找相关实体
	allEntities := make(map[string]bool)
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)

	mode, _ := ctx.Value("rag_mode").(QueryMode)

	for _, e := range entities {
		entityName := e
		g.Go(func() error {
			// 检查该实体是否在图中有任何“非文档链接”的关系
			hasRelation := false

			// 检查出边
			res, _ := r.graph.Query().V(entityName).Out("").All(gCtx)
			for _, qr := range res {
				if qr.Predicate != "APPEARS_IN" {
					hasRelation = true
					break
				}
			}

			// 检查入边
			if !hasRelation {
				res, _ := r.graph.Query().V(entityName).In("").All(gCtx)
				for _, qr := range res {
					if qr.Predicate != "APPEARS_IN" {
						hasRelation = true
						break
					}
				}
			}

			if hasRelation {
				mu.Lock()
				allEntities[entityName] = true
				mu.Unlock()
			} else if mode != ModeGraph && r.vector != nil {
				// 如果没找到直接关联，通过向量搜索寻找最相关的文档，从而发现相关实体
				emb, err := r.embedder.Embed(gCtx, entityName)
				if err == nil {
					vecResults, err := r.vector.Search(gCtx, emb, VectorSearchOptions{Limit: 3})
					if err == nil {
						for _, res := range vecResults {
							if res.Score < 0.75 {
								continue
							}

							docID := res.Document.ID()
							// 查找链接到该文档的所有实体 (Subject --[APPEARS_IN]--> docID)
							linkedEntities, _ := r.graph.GetInNeighbors(gCtx, docID, "APPEARS_IN")
							mu.Lock()
							for _, le := range linkedEntities {
								if !allEntities[le] {
									allEntities[le] = true
									logrus.WithFields(logrus.Fields{
										"query_entity": entityName,
										"found_entity": le,
										"score":        res.Score,
									}).Debug("Expanded entity via vector search")
								}
							}
							mu.Unlock()
						}
					}
				}
			}
			return nil
		})
	}
	_ = g.Wait()

	// 2. 对所有实体进行深度优先遍历
	// 这里也可以并行化子图获取
	g, gCtx = errgroup.WithContext(ctx)
	type subgraphResult struct {
		data *GraphData
		err  error
	}
	subgraphResults := make(chan subgraphResult, len(allEntities))

	for entityName := range allEntities {
		en := entityName
		g.Go(func() error {
			subgraph, err := r.GetSubgraph(gCtx, en, depth)
			subgraphResults <- subgraphResult{data: subgraph, err: err}
			return nil
		})
	}

	go func() {
		_ = g.Wait()
		close(subgraphResults)
	}()

	for res := range subgraphResults {
		if res.err != nil || res.data == nil {
			continue
		}
		for _, e := range res.data.Entities {
			if !entityMap[e.Name] {
				result.Entities = append(result.Entities, e)
				entityMap[e.Name] = true
			}
		}
		for _, rel := range res.data.Relationships {
			relKey := fmt.Sprintf("%s-%s-%s", rel.Source, rel.Relation, rel.Target)
			if !relMap[relKey] {
				result.Relationships = append(result.Relationships, rel)
				relMap[relKey] = true
			}
		}
	}

	// 打印召回的知识图谱
	if len(result.Entities) > 0 {
		logrus.WithFields(logrus.Fields{
			"query":           query,
			"entities_count":  len(result.Entities),
			"relations_count": len(result.Relationships),
		}).Info("Graph search recalled knowledge graph")
		for _, rel := range result.Relationships {
			logrus.Infof("  Graph Recalled: %s -[%s]-> %s", rel.Source, rel.Relation, rel.Target)
		}
	}

	return result, nil
}

// GetSubgraph 获取子图
func (r *LightRAG) GetSubgraph(ctx context.Context, nodeID string, depth int) (*GraphData, error) {
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}
	if r.graph == nil {
		return nil, fmt.Errorf("graph database not available")
	}

	if depth <= 0 {
		depth = 1
	}

	result := &GraphData{
		Entities:      make([]Entity, 0),
		Relationships: make([]Relationship, 0),
	}

	entityMap := make(map[string]bool)
	relMap := make(map[string]bool)
	var mu sync.Mutex

	currentLevelNodes := []string{nodeID}
	entityMap[nodeID] = true
	result.Entities = append(result.Entities, Entity{Name: nodeID})

	for d := 1; d <= depth; d++ {
		nextLevelNodes := make(map[string]bool)
		g, gCtx := errgroup.WithContext(ctx)

		for _, node := range currentLevelNodes {
			n := node
			g.Go(func() error {
				// 获取所有关系
				query := r.graph.Query().V(n)
				res, err := query.Both().All(gCtx)
				if err != nil {
					return nil
				}

				mu.Lock()
				defer mu.Unlock()

				for _, qr := range res {
					if qr.Predicate == "APPEARS_IN" {
						continue
					}

					target := qr.Object
					if target == n {
						target = qr.Subject
					}

					relKey := fmt.Sprintf("%s-%s-%s", qr.Subject, qr.Predicate, qr.Object)
					if !relMap[relKey] {
						result.Relationships = append(result.Relationships, Relationship{
							Source:   qr.Subject,
							Target:   qr.Object,
							Relation: qr.Predicate,
						})
						relMap[relKey] = true
					}

					if !entityMap[target] {
						result.Entities = append(result.Entities, Entity{Name: target})
						entityMap[target] = true
						nextLevelNodes[target] = true
					}
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			break
		}

		if len(nextLevelNodes) == 0 {
			break
		}

		currentLevelNodes = make([]string, 0, len(nextLevelNodes))
		for n := range nextLevelNodes {
			currentLevelNodes = append(currentLevelNodes, n)
		}
	}

	return result, nil
}

// Wait 等待所有后台任务完成
func (r *LightRAG) Wait() {
	r.wg.Wait()
}

// FinalizeStorages 关闭存储资源
func (r *LightRAG) FinalizeStorages(ctx context.Context) error {
	// 等待所有后台任务完成
	r.wg.Wait()

	if r.fulltext != nil {
		r.fulltext.Close()
	}
	if r.vector != nil {
		r.vector.Close()
	}
	if r.db != nil {
		return r.db.Close(ctx)
	}
	return nil
}

func matchesFilters(docData map[string]any, filters map[string]any) bool {
	if filters == nil || len(filters) == 0 {
		return true
	}
	for k, v := range filters {
		actual, ok := docData[k]
		if !ok || actual != v {
			return false
		}
	}
	return true
}
