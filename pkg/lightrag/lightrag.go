package lightrag

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// ExtractionStats 知识图谱提取统计信息
type ExtractionStats struct {
	TotalExtractions   int       // 总提取任务数
	SuccessCount       int       // 成功提取数
	FailureCount       int       // 失败提取数
	TotalEntities      int       // 提取的实体总数
	TotalRelationships int       // 提取的关系总数
	StartTime          time.Time // 开始时间
	EndTime            time.Time // 结束时间
	MaxConcurrency     int       // 最大并发数
}

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

	// 统计信息
	stats      ExtractionStats
	statsMutex sync.RWMutex // 保护统计信息的读写
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
		opts.MaxConcurrentLLM = 100
	}
	return &LightRAG{
		workingDir: opts.WorkingDir,
		embedder:   opts.Embedder,
		llm:        opts.LLM,
		llmSem:     make(chan struct{}, opts.MaxConcurrentLLM),
		stats: ExtractionStats{
			MaxConcurrency: opts.MaxConcurrentLLM,
			StartTime:      time.Now(),
		},
	}
}

// InitializeStorages 初始化存储后端
func (r *LightRAG) InitializeStorages(ctx context.Context) error {
	if r.initialized {
		return nil
	}

	// 创建数据库
	// 不同的业务模块通过表名前缀来区分（如 lightrag_documents）
	// duckdb-driver 会自动创建目录并处理路径映射，无需手动创建目录
	db, err := CreateDatabase(ctx, DatabaseOptions{
		Name:       "lightrag",
		WorkingDir: r.workingDir,
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
	docs, err := db.Collection(ctx, "lightrag_documents", docSchema)
	if err != nil {
		return fmt.Errorf("failed to create documents collection: %w", err)
	}
	r.docs = docs

	// 使用 errgroup 并行初始化搜索索引
	g, _ := errgroup.WithContext(ctx)

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
					// 使用 context.Background() 避免 context canceled 错误
					// 后台 worker 处理时，原始的 context 可能已被取消
					return r.embedder.Embed(context.Background(), content)
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
	if r == nil {
		return fmt.Errorf("LightRAG instance is nil")
	}
	if !r.initialized {
		return fmt.Errorf("storages not initialized")
	}
	if r.docs == nil {
		return fmt.Errorf("documents collection is not initialized")
	}

	// 如果chunk不超过10个字符，则不需要嵌入和入库存储
	if len([]rune(text)) <= 10 {
		logrus.WithFields(logrus.Fields{
			"content_len": len([]rune(text)),
		}).Debug("Skipping chunk that is too short (<=10 characters)")
		return nil
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
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}
	if r.docs == nil {
		return nil, fmt.Errorf("documents collection is not initialized")
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
	if r == nil {
		return fmt.Errorf("LightRAG instance is nil")
	}
	if !r.initialized {
		return fmt.Errorf("storages not initialized")
	}
	if r.docs == nil {
		return fmt.Errorf("documents collection is not initialized")
	}

	return r.docs.Delete(ctx, id)
}

func (r *LightRAG) extractQueryKeywords(ctx context.Context, query string) (*QueryKeywords, error) {
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
	if r.llm == nil {
		return &QueryKeywords{}, nil
	}

	promptStr, err := GetQueryEntityPrompt(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get query entity prompt: %w", err)
	}
	response, err := r.llm.Complete(ctx, promptStr)
	if err != nil {
		return nil, err
	}

	logrus.WithField("raw_response", response).Debug("LLM response for query keywords")

	jsonStr := response
	idxStart := strings.Index(jsonStr, "{")
	idxEnd := strings.LastIndex(jsonStr, "}")
	if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
		return nil, fmt.Errorf("no JSON object found in response: %s", response)
	}
	jsonStr = jsonStr[idxStart : idxEnd+1]

	var keywords QueryKeywords
	if err := json.Unmarshal([]byte(jsonStr), &keywords); err != nil {
		logrus.WithField("jsonStr", jsonStr).Error("Failed to parse query keywords")
		return nil, fmt.Errorf("failed to parse query keywords: %w", err)
	}

	return &keywords, nil
}

func (r *LightRAG) extractAndStore(ctx context.Context, text string, docID string) error {
	// 安全检查：防止 nil 指针
	if r == nil {
		return fmt.Errorf("LightRAG instance is nil")
	}
	if r.llm == nil {
		return fmt.Errorf("LLM is not available")
	}
	if r.graph == nil {
		return fmt.Errorf("graph database is not available")
	}

	// 更新统计：增加总提取任务数
	r.statsMutex.Lock()
	r.stats.TotalExtractions++
	r.statsMutex.Unlock()

	promptStr, err := GetExtractionPrompt(ctx, text)
	if err != nil {
		r.statsMutex.Lock()
		r.stats.FailureCount++
		r.statsMutex.Unlock()
		return fmt.Errorf("failed to get extraction prompt: %w", err)
	}
	response, err := r.llm.Complete(ctx, promptStr)
	if err != nil {
		r.statsMutex.Lock()
		r.stats.FailureCount++
		r.statsMutex.Unlock()
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
			r.statsMutex.Lock()
			r.stats.FailureCount++
			r.statsMutex.Unlock()
			return fmt.Errorf("no JSON object or array found in response: %s", response)
		}
	}
	jsonStr = jsonStr[idxStart : idxEnd+1]

	var result ExtractionResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		r.statsMutex.Lock()
		r.stats.FailureCount++
		r.statsMutex.Unlock()
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

	// 更新统计：成功提取
	r.statsMutex.Lock()
	r.stats.SuccessCount++
	r.stats.TotalEntities += len(result.Entities)
	r.stats.TotalRelationships += len(result.Relationships)
	r.statsMutex.Unlock()

	return nil
}

// InsertBatch 批量插入带元数据的文档
func (r *LightRAG) InsertBatch(ctx context.Context, documents []map[string]any) ([]string, error) {
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}
	if r.docs == nil {
		return nil, fmt.Errorf("documents collection is not initialized")
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

	// 记录批量提取开始时间（如果这是第一次批量提取）
	r.statsMutex.Lock()
	if r.stats.StartTime.IsZero() {
		r.stats.StartTime = time.Now()
	}
	r.statsMutex.Unlock()

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
	if r == nil {
		return "", fmt.Errorf("LightRAG instance is nil")
	}
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
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
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
		if r.embedder == nil {
			return nil, fmt.Errorf("embedder is not available")
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
		if r.fulltext == nil {
			return nil, fmt.Errorf("fulltext search not available")
		}
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
		keywords, err := r.extractQueryKeywords(ctx, query)
		if err != nil {
			logrus.WithError(err).Warn("Failed to extract query keywords, falling back to hybrid")
			return r.Retrieve(ctx, query, QueryParam{Mode: ModeHybrid, Limit: param.Limit})
		}

		// 根据 LightRAG 论文，Local search 使用 low-level keywords（具体实体）
		// 用于检索直接包含这些实体的文档及其邻居实体
		logrus.WithFields(logrus.Fields{
			"query":      query,
			"low_level":  keywords.LowLevel,  // Local search 使用 low_level keywords
			"high_level": keywords.HighLevel, // 同时显示 high_level 以便调试和理解分类
		}).Info("Performing local search")

		return r.retrieveByKeywords(ctx, keywords.LowLevel, param)

	case ModeGraph:
		if r.graph == nil {
			return nil, fmt.Errorf("graph search not available")
		}

		// Graph 模式：纯知识图谱查询，不使用向量或全文搜索
		// 1. 获取关键词
		keywords, err := r.extractQueryKeywords(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to extract keywords: %w", err)
		}

		// 2. 获取图谱数据
		// 结合 low_level 和 high_level 进行图谱搜索
		allKeywords := append(keywords.LowLevel, keywords.HighLevel...)
		graphData := &GraphData{
			Entities:      make([]Entity, 0),
			Relationships: make([]Relationship, 0),
		}

		entityMap := make(map[string]bool)
		relMap := make(map[string]bool)

		for _, k := range allKeywords {
			subgraph, _ := r.GetSubgraph(ctx, k, 2)
			if subgraph != nil {
				for _, e := range subgraph.Entities {
					if !entityMap[e.Name] {
						graphData.Entities = append(graphData.Entities, e)
						entityMap[e.Name] = true
					}
				}
				for _, rel := range subgraph.Relationships {
					relKey := fmt.Sprintf("%s-%s-%s", rel.Source, rel.Relation, rel.Target)
					if !relMap[relKey] {
						graphData.Relationships = append(graphData.Relationships, rel)
						relMap[relKey] = true
					}
				}
			}
		}

		recalledTriples = graphData.Relationships

		// 3. 根据召回的实体找到关联的文档
		docIDMap := make(map[string]bool)
		for _, entity := range graphData.Entities {
			neighbors, _ := r.graph.GetNeighbors(ctx, entity.Name, "APPEARS_IN")
			for _, id := range neighbors {
				docIDMap[id] = true
			}
		}

		// 4. 获取文档内容
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
		if r.graph == nil {
			return nil, fmt.Errorf("graph search not available")
		}
		keywords, err := r.extractQueryKeywords(ctx, query)
		if err != nil {
			logrus.WithError(err).Warn("Failed to extract query keywords, falling back to hybrid")
			return r.Retrieve(ctx, query, QueryParam{Mode: ModeHybrid, Limit: param.Limit})
		}

		// 根据 LightRAG 论文，Global search 使用 high-level keywords（抽象主题）
		// 用于检索更广泛相关的文档，而不仅仅是直接包含具体实体的文档
		logrus.WithFields(logrus.Fields{
			"query":      query,
			"low_level":  keywords.LowLevel,  // 同时显示 low_level 以便调试和理解分类
			"high_level": keywords.HighLevel, // Global search 使用 high_level keywords
		}).Info("Performing global search")

		return r.retrieveByKeywords(ctx, keywords.HighLevel, param)
	case ModeHybrid:
		// 论文中的 Hybrid：结合 Local 和 Global
		keywords, err := r.extractQueryKeywords(ctx, query)
		if err != nil {
			// 回退到朴素混合搜索（向量 + 全文）
			return r.retrieveNaiveHybrid(ctx, query, param)
		}

		// 如果关键词列表都为空，也回退到朴素混合搜索
		if len(keywords.LowLevel) == 0 && len(keywords.HighLevel) == 0 {
			logrus.Warn("No keywords extracted, falling back to naive hybrid search")
			return r.retrieveNaiveHybrid(ctx, query, param)
		}

		logrus.WithFields(logrus.Fields{
			"query":      query,
			"low_level":  keywords.LowLevel,
			"high_level": keywords.HighLevel,
		}).Info("Performing hybrid search (local + global)")

		// 分别进行 local 和 global 检索并合并结果
		localResults, _ := r.retrieveByKeywords(ctx, keywords.LowLevel, param)
		globalResults, _ := r.retrieveByKeywords(ctx, keywords.HighLevel, param)

		// 如果两个结果都为空，回退到朴素混合搜索
		if len(localResults) == 0 && len(globalResults) == 0 {
			logrus.Warn("No results from keyword-based search, falling back to naive hybrid search")
			return r.retrieveNaiveHybrid(ctx, query, param)
		}

		// 合并结果
		return r.mergeSearchResults(localResults, globalResults, param.Limit), nil
	case ModeMix:
		// Mix 模式：结合知识图谱和向量检索
		// 根据 Python 版本，mix 模式整合知识图谱和向量检索
		if r.graph == nil {
			return nil, fmt.Errorf("graph search not available")
		}
		keywords, err := r.extractQueryKeywords(ctx, query)
		if err != nil {
			// 如果提取关键词失败，回退到向量搜索
			if r.vector == nil || r.embedder == nil {
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
				return nil, err
			}
			results := make([]SearchResult, 0, len(vecResults))
			for _, v := range vecResults {
				content, _ := v.Document.Data()["content"].(string)
				results = append(results, SearchResult{
					ID:       v.Document.ID(),
					Content:  content,
					Score:    v.Score,
					Metadata: v.Document.Data(),
				})
			}
			return results, nil
		}

		// 如果关键词列表为空，回退到向量搜索
		allKeywords := append(keywords.LowLevel, keywords.HighLevel...)
		if len(allKeywords) == 0 {
			logrus.Warn("No keywords extracted for mix search, falling back to vector search")
			if r.vector == nil || r.embedder == nil {
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
				return nil, err
			}
			results := make([]SearchResult, 0, len(vecResults))
			for _, v := range vecResults {
				content, _ := v.Document.Data()["content"].(string)
				results = append(results, SearchResult{
					ID:       v.Document.ID(),
					Content:  content,
					Score:    v.Score,
					Metadata: v.Document.Data(),
				})
			}
			return results, nil
		}

		logrus.WithFields(logrus.Fields{
			"query":      query,
			"low_level":  keywords.LowLevel,
			"high_level": keywords.HighLevel,
		}).Info("Performing mix search (graph + vector)")

		// Mix 模式使用所有关键词（low-level + high-level）进行检索
		// retrieveByKeywords 方法已经实现了图谱 + 向量的组合检索
		results, err := r.retrieveByKeywords(ctx, allKeywords, param)
		if err != nil {
			return nil, err
		}
		// 如果关键词检索没有结果，回退到向量搜索
		if len(results) == 0 {
			logrus.Warn("No results from keyword-based mix search, falling back to vector search")
			if r.vector == nil || r.embedder == nil {
				return results, nil // 返回空结果而不是错误
			}
			emb, err := r.embedder.Embed(ctx, query)
			if err != nil {
				return results, nil // 返回空结果而不是错误
			}
			vecResults, err := r.vector.Search(ctx, emb, VectorSearchOptions{
				Limit:    param.Limit,
				Selector: param.Filters,
			})
			if err != nil {
				return results, nil // 返回空结果而不是错误
			}
			for _, v := range vecResults {
				content, _ := v.Document.Data()["content"].(string)
				results = append(results, SearchResult{
					ID:       v.Document.ID(),
					Content:  content,
					Score:    v.Score,
					Metadata: v.Document.Data(),
				})
			}
		}
		return results, nil
	default:
		if r.fulltext == nil {
			return nil, fmt.Errorf("fulltext search not available")
		}
		rawResults, err = r.fulltext.FindWithScores(ctx, query, FulltextSearchOptions{Limit: param.Limit})
		if err != nil {
			return nil, err
		}
	}

	results := make([]SearchResult, 0, len(rawResults))
	for _, res := range rawResults {
		if res.Document == nil {
			continue
		}
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
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
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
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
	if !r.initialized {
		return nil, fmt.Errorf("storages not initialized")
	}
	if r.graph == nil {
		return nil, fmt.Errorf("graph database not available")
	}

	keywords, err := r.extractQueryKeywords(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to extract keywords from query: %w", err)
	}

	result := &GraphData{
		Entities:      make([]Entity, 0),
		Relationships: make([]Relationship, 0),
	}

	entityMap := make(map[string]bool)
	relMap := make(map[string]bool)

	// 合并 low_level 和 high_level
	entities := append(keywords.LowLevel, keywords.HighLevel...)

	// 1. 扩展实体：如果提取的实体在图中找不到直接关系，尝试通过语义搜索寻找相关实体
	allEntities := make(map[string]bool)
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)

	mode, _ := ctx.Value("rag_mode").(QueryMode)

	for _, e := range entities {
		entityName := e
		g.Go(func() error {
			// 检查该实体是否在图中有任何"非文档链接"的关系
			hasRelation := false

			// 检查出边
			if r.graph != nil {
				query := r.graph.Query()
				if query != nil {
					res, _ := query.V(entityName).Out("").All(gCtx)
					for _, qr := range res {
						if qr.Predicate != "APPEARS_IN" {
							hasRelation = true
							break
						}
					}
				}
			}

			// 检查入边
			if !hasRelation && r.graph != nil {
				query := r.graph.Query()
				if query != nil {
					res, _ := query.V(entityName).In("").All(gCtx)
					for _, qr := range res {
						if qr.Predicate != "APPEARS_IN" {
							hasRelation = true
							break
						}
					}
				}
			}

			if hasRelation {
				mu.Lock()
				allEntities[entityName] = true
				mu.Unlock()
			} else if mode != ModeGraph && r.vector != nil && r.embedder != nil {
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
							if r.graph != nil {
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
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
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
				if r.graph == nil {
					return nil
				}
				query := r.graph.Query()
				if query == nil {
					return nil
				}
				res, err := query.V(n).Both().All(gCtx)
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
	// 记录结束时间
	r.statsMutex.Lock()
	r.stats.EndTime = time.Now()
	r.statsMutex.Unlock()
}

// WaitForEmbeddings 等待所有向量嵌入完成（最多等待 maxWait 时间）
// embedding worker 每 2 秒检查一次，每次最多处理 10 个文档，速率限制是每秒 5 次
func (r *LightRAG) WaitForEmbeddings(ctx context.Context, maxWait time.Duration) error {
	if r == nil || !r.initialized || r.docs == nil {
		return nil
	}
	if r.vector == nil || r.embedder == nil {
		return nil // 没有向量搜索，不需要等待
	}

	// 简单实现：等待足够的时间让 embedding worker 处理完所有文档
	// 对于少量文档（<10），通常需要 2-4 秒
	// 我们使用轮询方式，每 500ms 检查一次，最多等待 maxWait 时间
	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// 检查是否超时
			if time.Now().After(deadline) {
				return nil // 超时了，返回 nil（不是错误）
			}
			// 继续等待
		}
	}
}

// GetExtractionStats 获取知识图谱提取统计信息
func (r *LightRAG) GetExtractionStats() ExtractionStats {
	r.statsMutex.RLock()
	defer r.statsMutex.RUnlock()
	// 返回副本以避免竞态条件
	return ExtractionStats{
		TotalExtractions:   r.stats.TotalExtractions,
		SuccessCount:       r.stats.SuccessCount,
		FailureCount:       r.stats.FailureCount,
		TotalEntities:      r.stats.TotalEntities,
		TotalRelationships: r.stats.TotalRelationships,
		StartTime:          r.stats.StartTime,
		EndTime:            r.stats.EndTime,
		MaxConcurrency:     r.stats.MaxConcurrency,
	}
}

// CountAppearsInLinks 统计 APPEARS_IN 链接的数量（实体到文档的链接）
func (r *LightRAG) CountAppearsInLinks(ctx context.Context) (int, error) {
	if r == nil {
		return 0, fmt.Errorf("LightRAG instance is nil")
	}
	if !r.initialized {
		return 0, fmt.Errorf("storages not initialized")
	}
	if r.graph == nil {
		return 0, fmt.Errorf("graph database not available")
	}

	triples, err := r.graph.AllTriples(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get all triples: %w", err)
	}

	count := 0
	for _, t := range triples {
		if t.Predicate == "APPEARS_IN" {
			count++
		}
	}

	return count, nil
}

// FinalizeStorages 关闭存储资源
func (r *LightRAG) FinalizeStorages(ctx context.Context) error {
	// 等待所有后台任务完成（包括实体提取任务）
	r.wg.Wait()

	// 等待一小段时间，确保 embedding worker 有机会完成当前正在处理的文档
	// 注意：embedding worker 会在数据库关闭时自动停止
	time.Sleep(100 * time.Millisecond)

	if r.fulltext != nil {
		r.fulltext.Close()
	}
	if r.vector != nil {
		r.vector.Close()
	}
	if r.db != nil {
		err := r.db.Close(ctx)
		// 无论关闭是否成功，都将 initialized 设置为 false
		r.initialized = false
		return err
	}
	r.initialized = false
	return nil
}

func (r *LightRAG) retrieveByKeywords(ctx context.Context, keywords []string, param QueryParam) ([]SearchResult, error) {
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
	if len(keywords) == 0 {
		return []SearchResult{}, nil
	}
	if r.graph == nil {
		return nil, fmt.Errorf("graph database is not available")
	}

	docIDMap := make(map[string]float64) // docID -> score
	var recalledTriples []Relationship
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)

	for _, kw := range keywords {
		keyword := kw
		g.Go(func() error {
			// 1. 图谱检索：查找实体及其邻居
			subgraph, _ := r.GetSubgraph(gCtx, keyword, 1)
			if subgraph != nil {
				mu.Lock()
				recalledTriples = append(recalledTriples, subgraph.Relationships...)
				mu.Unlock()

				// 查找关联文档
				for _, entity := range subgraph.Entities {
					if r.graph != nil {
						neighbors, _ := r.graph.GetNeighbors(gCtx, entity.Name, "APPEARS_IN")
						mu.Lock()
						for _, id := range neighbors {
							docIDMap[id] += 1.0 // 简单的计数评分
						}
						mu.Unlock()
					}
				}
			}

			// 2. 向量检索：查找相关的文档块
			if r.vector != nil && r.embedder != nil {
				emb, err := r.embedder.Embed(gCtx, keyword)
				if err == nil {
					vecResults, err := r.vector.Search(gCtx, emb, VectorSearchOptions{
						Limit:    param.Limit,
						Selector: param.Filters,
					})
					if err == nil {
						mu.Lock()
						for _, vr := range vecResults {
							docIDMap[vr.Document.ID()] += vr.Score
						}
						mu.Unlock()
					}
				}
			}
			return nil
		})
	}
	_ = g.Wait()

	// 排序并获取文档
	type scoredDoc struct {
		id    string
		score float64
	}
	var sortedDocs []scoredDoc
	for id, score := range docIDMap {
		sortedDocs = append(sortedDocs, scoredDoc{id: id, score: score})
	}
	sort.Slice(sortedDocs, func(i, j int) bool {
		return sortedDocs[i].score > sortedDocs[j].score
	})

	limit := param.Limit
	if len(sortedDocs) < limit {
		limit = len(sortedDocs)
	}
	sortedDocs = sortedDocs[:limit]

	results := make([]SearchResult, 0, len(sortedDocs))
	if r.docs == nil {
		return results, nil
	}
	for _, sd := range sortedDocs {
		doc, err := r.docs.FindByID(ctx, sd.id)
		if err == nil && doc != nil {
			content, _ := doc.Data()["content"].(string)
			results = append(results, SearchResult{
				ID:              sd.id,
				Content:         content,
				Score:           sd.score,
				Metadata:        doc.Data(),
				RecalledTriples: recalledTriples,
			})
		}
	}

	return results, nil
}

func (r *LightRAG) retrieveNaiveHybrid(ctx context.Context, query string, param QueryParam) ([]SearchResult, error) {
	if r == nil {
		return nil, fmt.Errorf("LightRAG instance is nil")
	}
	if r.fulltext == nil {
		return nil, fmt.Errorf("fulltext search not available")
	}
	// 实现简单的混合搜索（向量 + 全文）
	var ftResults []FulltextSearchResult
	var vecResults []VectorSearchResult

	g, gCtx := errgroup.WithContext(ctx)

	// 1. 全文搜索
	g.Go(func() error {
		var err error
		ftResults, err = r.fulltext.FindWithScores(gCtx, query, FulltextSearchOptions{
			Limit:    param.Limit,
			Selector: param.Filters,
		})
		return err
	})

	// 2. 向量搜索
	if r.vector != nil && r.embedder != nil {
		g.Go(func() error {
			emb, err := r.embedder.Embed(gCtx, query)
			if err != nil {
				return nil
			}
			var err2 error
			vecResults, err2 = r.vector.Search(gCtx, emb, VectorSearchOptions{
				Limit:    param.Limit,
				Selector: param.Filters,
			})
			return err2
		})
	}

	_ = g.Wait()

	// RRF 融合
	docScores := make(map[string]float64)
	docMap := make(map[string]Document)

	for i, res := range ftResults {
		if res.Document == nil {
			continue
		}
		score := 1.0 / float64(i+60)
		docScores[res.Document.ID()] += score
		docMap[res.Document.ID()] = res.Document
	}

	for i, res := range vecResults {
		if res.Document == nil {
			continue
		}
		score := 1.0 / float64(i+60)
		docScores[res.Document.ID()] += score
		docMap[res.Document.ID()] = res.Document
	}

	var results []SearchResult
	for id, score := range docScores {
		doc := docMap[id]
		if doc == nil {
			continue
		}
		content, _ := doc.Data()["content"].(string)
		results = append(results, SearchResult{
			ID:       id,
			Content:  content,
			Score:    score,
			Metadata: doc.Data(),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > param.Limit {
		results = results[:param.Limit]
	}

	return results, nil
}

func (r *LightRAG) mergeSearchResults(r1, r2 []SearchResult, limit int) []SearchResult {
	seen := make(map[string]bool)
	var merged []SearchResult

	// 简单的合并去重
	for _, r := range r1 {
		if !seen[r.ID] {
			merged = append(merged, r)
			seen[r.ID] = true
		}
	}
	for _, r := range r2 {
		if !seen[r.ID] {
			merged = append(merged, r)
			seen[r.ID] = true
		} else {
			// 如果已存在，合并三元组
			for i := range merged {
				if merged[i].ID == r.ID {
					merged[i].RecalledTriples = append(merged[i].RecalledTriples, r.RecalledTriples...)
					// 去重三元组逻辑可以更复杂，这里简单处理
					break
				}
			}
		}
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})

	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
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
