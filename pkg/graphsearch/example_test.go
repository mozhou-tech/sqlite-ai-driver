package graphsearch_test

import (
	"context"
	"fmt"
	"log"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

// SimpleEmbedder 简单的嵌入生成器示例（用于测试）
type SimpleEmbedder struct {
	dimensions int
}

func NewSimpleEmbedder(dims int) *SimpleEmbedder {
	if dims <= 0 {
		dims = 768
	}
	return &SimpleEmbedder{dimensions: dims}
}

func (e *SimpleEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	vec := make([]float64, e.dimensions)
	// 极简实现：取前 N 个字符的 ASCII 值归一化
	for i := 0; i < len(text) && i < e.dimensions; i++ {
		vec[i] = float64(text[i]) / 255.0
	}
	return vec, nil
}

func (e *SimpleEmbedder) Dimensions() int {
	return e.dimensions
}

// Examplegraphsearch 展示如何使用 graphsearch 进行向量检索
func Examplegraphsearch() {
	ctx := context.Background()

	// 1. 创建 Embedder（实际使用时可以使用 OpenAI 或其他 embedding 服务）
	embedder := NewSimpleEmbedder(768)

	// 2. 创建 graphsearch 实例
	store, err := graphsearch.New(graphsearch.Options{
		Embedder:   embedder,
		WorkingDir: "./testdata",           // 工作目录，作为基础目录
		TableName:  "graphsearch_entities", // DuckDB 表名
	})
	if err != nil {
		log.Fatalf("Failed to create graphsearch: %v", err)
	}
	defer store.Close()

	// 3. 初始化存储
	if err := store.Initialize(ctx); err != nil {
		log.Fatalf("Failed to initialize graphsearch: %v", err)
	}

	// 4. 添加实体（会自动生成 embedding）
	fmt.Println("=== 添加实体 ===")
	entities := []struct {
		id       string
		name     string
		metadata map[string]any
	}{
		{"person1", "张三", map[string]any{"type": "person", "age": 30, "job": "软件工程师"}},
		{"person2", "李四", map[string]any{"type": "person", "age": 28, "job": "产品经理"}},
		{"person3", "王五", map[string]any{"type": "person", "age": 35, "job": "架构师"}},
		{"company1", "科技公司A", map[string]any{"type": "company", "industry": "互联网"}},
		{"company2", "科技公司B", map[string]any{"type": "company", "industry": "人工智能"}},
		{"project1", "AI项目", map[string]any{"type": "project", "status": "进行中"}},
	}

	for _, e := range entities {
		if err := store.AddEntity(ctx, e.id, e.name, e.metadata); err != nil {
			log.Printf("Failed to add entity %s: %v", e.id, err)
		} else {
			fmt.Printf("✓ 添加实体: %s (%s)\n", e.name, e.id)
		}
	}

	// 5. 创建图谱关系
	fmt.Println("\n=== 创建图谱关系 ===")
	relationships := []struct {
		subject   string
		predicate string
		object    string
	}{
		{"person1", "works_at", "company1"},
		{"person2", "works_at", "company1"},
		{"person3", "works_at", "company2"},
		{"person1", "knows", "person2"},
		{"person2", "knows", "person3"},
		{"person1", "leads", "project1"},
		{"company1", "has_project", "project1"},
		{"company2", "competes_with", "company1"},
	}

	for _, rel := range relationships {
		if err := store.Link(ctx, rel.subject, rel.predicate, rel.object); err != nil {
			log.Printf("Failed to link %s --[%s]--> %s: %v", rel.subject, rel.predicate, rel.object, err)
		} else {
			fmt.Printf("✓ %s --[%s]--> %s\n", rel.subject, rel.predicate, rel.object)
		}
	}

	// 6. 使用向量检索图谱实体
	fmt.Println("\n=== 向量检索图谱实体 ===")
	queries := []string{
		"软件工程师",
		"科技公司",
		"AI项目负责人",
	}

	for _, query := range queries {
		fmt.Printf("\n查询: \"%s\"\n", query)
		fmt.Println("---")

		// 执行语义检索
		// limit: 返回前 5 个最相似的实体
		// maxDepth: 获取每个实体周围深度为 2 的子图
		results, err := store.SemanticSearch(ctx, query, 5, 2)
		if err != nil {
			log.Printf("Failed to search: %v", err)
			continue
		}

		if len(results) == 0 {
			fmt.Println("  未找到相关实体")
			continue
		}

		// 显示搜索结果
		for i, result := range results {
			fmt.Printf("\n[结果 %d] 实体: %s (ID: %s)\n", i+1, result.EntityName, result.EntityID)
			fmt.Printf("  相似度分数: %.4f\n", result.Score)
			fmt.Printf("  元数据: %v\n", result.Metadata)

			// 显示该实体在图谱中的关系
			if len(result.Triples) > 0 {
				fmt.Printf("  相关关系 (%d 条):\n", len(result.Triples))
				for _, triple := range result.Triples {
					fmt.Printf("    • %s --[%s]--> %s\n", triple.Subject, triple.Predicate, triple.Object)
				}
			} else {
				fmt.Printf("  相关关系: 无\n")
			}
		}
	}

	// 7. 获取特定实体的子图
	fmt.Println("\n=== 获取实体子图 ===")
	entityID := "person1"
	triples, err := store.GetSubgraph(ctx, entityID, 2)
	if err != nil {
		log.Printf("Failed to get subgraph: %v", err)
	} else {
		fmt.Printf("实体 %s 的子图（深度 2）:\n", entityID)
		for _, triple := range triples {
			fmt.Printf("  %s --[%s]--> %s\n", triple.Subject, triple.Predicate, triple.Object)
		}
	}

	// 8. 获取实体的邻居
	fmt.Println("\n=== 获取实体邻居 ===")
	neighbors, err := store.GetNeighbors(ctx, "person1", "")
	if err != nil {
		log.Printf("Failed to get neighbors: %v", err)
	} else {
		fmt.Printf("person1 的所有邻居: %v\n", neighbors)
	}

	// 9. 查找路径
	fmt.Println("\n=== 查找路径 ===")
	paths, err := store.FindPath(ctx, "person1", "company2", 5, "")
	if err != nil {
		log.Printf("Failed to find path: %v", err)
	} else {
		fmt.Printf("从 person1 到 company2 的路径:\n")
		for i, path := range paths {
			fmt.Printf("  路径 %d: %v\n", i+1, path)
		}
	}
}

// TestSemanticSearchUsage 展示语义检索的详细用法
func TestSemanticSearchUsage(t *testing.T) {
	ctx := context.Background()

	// 创建 embedder
	embedder := NewSimpleEmbedder(768)

	// 创建 graphsearch
	store, err := graphsearch.New(graphsearch.Options{
		Embedder:   embedder,
		WorkingDir: "./testdata", // 工作目录，作为基础目录
	})
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	if err := store.Initialize(ctx); err != nil {
		log.Fatal(err)
	}

	// 添加一些实体
	store.AddEntity(ctx, "e1", "机器学习专家", map[string]any{"skill": "ML"})
	store.AddEntity(ctx, "e2", "深度学习工程师", map[string]any{"skill": "DL"})
	store.AddEntity(ctx, "e3", "数据科学家", map[string]any{"skill": "Data Science"})

	// 创建关系
	store.Link(ctx, "e1", "collaborates_with", "e2")
	store.Link(ctx, "e2", "collaborates_with", "e3")

	// 执行语义检索
	// 查询: "AI技术专家"
	// limit: 返回前 3 个最相似的实体
	// maxDepth: 获取每个实体周围深度为 1 的子图
	results, err := store.SemanticSearch(ctx, "AI技术专家", 3, 1)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("语义检索结果:")
	for _, result := range results {
		fmt.Printf("- %s (相似度: %.4f)\n", result.EntityName, result.Score)
		fmt.Printf("  关系数: %d\n", len(result.Triples))
	}
}
