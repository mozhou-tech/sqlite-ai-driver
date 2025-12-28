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
}

// Options LightRAG 配置选项
type Options struct {
	WorkingDir string
	Embedder   Embedder
	LLM        LLM
}

// New 创建 LightRAG 实例
func New(opts Options) *LightRAG {
	return &LightRAG{
		workingDir: opts.WorkingDir,
		embedder:   opts.Embedder,
		llm:        opts.LLM,
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

	// 初始化全文搜索
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

	// 初始化向量搜索
	if r.embedder != nil {
		vector, err := AddVectorSearch(docs, VectorSearchConfig{
			Identifier: "docs_vector",
			DocToEmbedding: func(doc map[string]any) ([]float64, error) {
				content, _ := doc["content"].(string)
				return r.embedder.Embed(ctx, content)
			},
			Dimensions: r.embedder.Dimensions(),
		})
		if err != nil {
			return fmt.Errorf("failed to add vector search: %w", err)
		}
		r.vector = vector
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

	// 尝试解析 JSON
	jsonStr := response
	idxStart := strings.Index(jsonStr, "{")
	idxEnd := strings.LastIndex(jsonStr, "}")
	if idxStart == -1 || idxEnd == -1 || idxEnd < idxStart {
		return fmt.Errorf("no JSON object found in response: %s", response)
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

	// 存储实体并将其实体链接到文档
	for _, entity := range result.Entities {
		logrus.WithFields(logrus.Fields{
			"entity": entity.Name,
			"doc_id": docID,
		}).Debug("Linking entity to document")
		// 链接实体到文档
		err := r.graph.Link(ctx, entity.Name, "APPEARS_IN", docID)
		if err != nil {
			logrus.WithError(err).Errorf("Failed to link entity %s to doc %s", entity.Name, docID)
		}
	}

	// 存储关系
	for _, rel := range result.Relationships {
		logrus.WithFields(logrus.Fields{
			"source":   rel.Source,
			"relation": rel.Relation,
			"target":   rel.Target,
		}).Debug("Storing relationship in graph")
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
		// 批量插入时也进行图谱提取
		if r.llm != nil && r.graph != nil {
			content, _ := doc.Data()["content"].(string)
			docID := doc.ID()
			r.wg.Add(1)
			go func(c string, id string) {
				defer r.wg.Done()
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
	var err error

	switch param.Mode {
	case ModeVector:
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
	case ModeLocal, ModeGraph:
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
		}).Info("Extracted entities for graph search")

		docIDMap := make(map[string]bool)
		for _, entity := range entities {
			neighbors, _ := r.graph.GetNeighbors(ctx, entity, "APPEARS_IN")
			if len(neighbors) > 0 {
				logrus.WithFields(logrus.Fields{
					"entity":    entity,
					"doc_count": len(neighbors),
				}).Debug("Found documents directly linked to entity")
			}
			for _, id := range neighbors {
				docIDMap[id] = true
			}
			// 也考虑一度邻居关联的文档
			related, _ := r.graph.GetNeighbors(ctx, entity, "")
			for _, relNode := range related {
				if relNode == entity {
					continue
				}
				docNeighbors, _ := r.graph.GetNeighbors(ctx, relNode, "APPEARS_IN")
				if len(docNeighbors) > 0 {
					logrus.WithFields(logrus.Fields{
						"original_entity": entity,
						"related_node":    relNode,
						"doc_count":       len(docNeighbors),
					}).Debug("Found documents linked via related graph node")
				}
				for _, id := range docNeighbors {
					docIDMap[id] = true
				}
			}
		}

		logrus.WithField("total_unique_docs", len(docIDMap)).Info("Graph traversal completed")

		for id := range docIDMap {
			doc, _ := r.docs.FindByID(ctx, id)
			if doc != nil {
				rawResults = append(rawResults, FulltextSearchResult{
					Document: doc,
					Score:    1.0, // 图检索的基础分，后续可以改进
				})
			}
			if len(rawResults) >= param.Limit {
				break
			}
		}
	case ModeGlobal:
		// 全局搜索目前简化为：查找所有实体的共同关系或中心节点
		// 暂时退回到混合搜索或图遍历
		return r.Retrieve(ctx, query, QueryParam{Mode: ModeHybrid, Limit: param.Limit})
	case ModeHybrid:
		// 实现真正的混合搜索（向量 + 全文 + 可能的图）
		ftResults, err := r.fulltext.FindWithScores(ctx, query, FulltextSearchOptions{
			Limit:    param.Limit * 2,
			Selector: param.Filters,
		})
		if err != nil {
			logrus.WithError(err).Error("Fulltext search failed in hybrid mode")
			ftResults = []FulltextSearchResult{} // 确保不是 nil
		}
		logrus.WithField("count", len(ftResults)).Debug("Hybrid mode: Fulltext results")

		var vecResults []VectorSearchResult
		if r.vector != nil && r.embedder != nil {
			emb, err := r.embedder.Embed(ctx, query)
			if err == nil {
				vecResults, err = r.vector.Search(ctx, emb, VectorSearchOptions{
					Limit:    param.Limit * 2,
					Selector: param.Filters,
				})
				if err != nil {
					logrus.WithError(err).Error("Vector search failed in hybrid mode")
				}
			}
		}
		if vecResults == nil {
			vecResults = []VectorSearchResult{} // 确保不是 nil
		}
		logrus.WithField("count", len(vecResults)).Debug("Hybrid mode: Vector results")

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
			ID:       res.Document.ID(),
			Content:  content,
			Score:    res.Score,
			Metadata: res.Document.Data(),
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
	for _, e := range entities {
		allEntities[e] = true

		// 检查该实体是否在图中有任何“非文档链接”的关系
		hasRelation := false

		// 检查出边
		res, _ := r.graph.Query().V(e).Out("").All(ctx)
		for _, r := range res {
			if r.Predicate != "APPEARS_IN" {
				hasRelation = true
				break
			}
		}

		// 检查入边
		if !hasRelation {
			res, _ := r.graph.Query().V(e).In("").All(ctx)
			for _, r := range res {
				if r.Predicate != "APPEARS_IN" {
					hasRelation = true
					break
				}
			}
		}

		if !hasRelation && r.vector != nil {
			// 如果没找到直接关联，通过向量搜索寻找最相关的文档，从而发现相关实体
			emb, err := r.embedder.Embed(ctx, e)
			if err == nil {
				vecResults, err := r.vector.Search(ctx, emb, VectorSearchOptions{Limit: 3})
				if err == nil {
					for _, res := range vecResults {
						docID := res.Document.ID()
						// 查找链接到该文档的所有实体 (Subject --[APPEARS_IN]--> docID)
						linkedEntities, _ := r.graph.GetInNeighbors(ctx, docID, "APPEARS_IN")
						for _, le := range linkedEntities {
							if !allEntities[le] {
								allEntities[le] = true
								logrus.WithFields(logrus.Fields{
									"query_entity": e,
									"found_entity": le,
								}).Debug("Expanded entity via vector search")
							}
						}
					}
				}
			}
		}
	}

	// 2. 对所有实体进行深度优先遍历
	for entityName := range allEntities {
		subgraph, err := r.GetSubgraph(ctx, entityName, depth)
		if err != nil {
			continue
		}
		for _, e := range subgraph.Entities {
			if !entityMap[e.Name] {
				result.Entities = append(result.Entities, e)
				entityMap[e.Name] = true
			}
		}
		for _, rel := range subgraph.Relationships {
			relKey := fmt.Sprintf("%s-%s-%s", rel.Source, rel.Relation, rel.Target)
			if !relMap[relKey] {
				result.Relationships = append(result.Relationships, rel)
				relMap[relKey] = true
			}
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

	var traverse func(node string, currentDepth int)
	traverse = func(node string, currentDepth int) {
		if currentDepth > depth {
			return
		}

		if !entityMap[node] {
			result.Entities = append(result.Entities, Entity{Name: node})
			entityMap[node] = true
		}

		// 获取所有关系
		query := r.graph.Query().V(node)
		res, err := query.Both().All(ctx)
		if err == nil {
			for _, qr := range res {
				if qr.Predicate == "APPEARS_IN" {
					continue
				}

				target := qr.Object
				if target == node {
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

				if currentDepth < depth {
					traverse(target, currentDepth+1)
				}
			}
		}
	}

	traverse(nodeID, 1)
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
