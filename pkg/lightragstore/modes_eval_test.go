package lightrag

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func setupRAG(t *testing.T, workingDir string) (*LightRAG, func()) {
	ctx := context.Background()
	_ = os.RemoveAll(workingDir)

	embedder := NewSimpleEmbedder(768)
	llm := &SimpleLLM{}

	rag := New(Options{
		WorkingDir: workingDir,
		Embedder:   embedder,
		LLM:        llm,
	})

	if err := rag.InitializeStorages(ctx); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	return rag, func() {
		// 等待所有后台任务完成（包括实体提取任务）
		rag.Wait()
		// 关闭存储资源（这会停止 embedding worker 并等待其完成）
		rag.FinalizeStorages(ctx)
		os.RemoveAll(workingDir)
	}
}

func TestEvaluateModes(t *testing.T) {
	workingDir := "./test_modes_eval"
	rag, cleanup := setupRAG(t, workingDir)
	defer cleanup()

	ctx := context.Background()

	// 1. 插入测试文档
	docs := []string{
		"The Great Wall of China is a series of fortifications built across the historical northern borders of ancient Chinese states.",
		"Paris is the capital and most populous city of France.",
		"The Eiffel Tower is a wrought-iron lattice tower on the Champ de Mars in Paris.",
		"JavaScript is a high-level, often just-in-time compiled language that conforms to the ECMAScript specification.",
		"SQLiteAI is a NoSQL-database for Golang Applications.",
	}

	for _, d := range docs {
		if err := rag.Insert(ctx, d); err != nil {
			t.Fatalf("failed to insert doc: %v", err)
		}
	}

	// 等待异步图谱提取完成
	rag.Wait()

	// 等待向量嵌入完成（embedding worker 每 2 秒检查一次，每次最多处理 10 个文档）
	// 对于 5 个文档，需要等待足够的时间让 worker 处理完
	if err := rag.WaitForEmbeddings(ctx, 10*time.Second); err != nil {
		t.Logf("Warning: WaitForEmbeddings returned error: %v", err)
	}

	tests := []struct {
		name     string
		query    string
		mode     QueryMode
		expected []string
	}{
		{
			name:     "全文搜索评估 (Fulltext)",
			query:    "fortifications",
			mode:     ModeFulltext,
			expected: []string{"Great Wall"},
		},
		{
			name:     "向量搜索评估 (Vector)",
			query:    "French capital city",
			mode:     ModeVector,
			expected: []string{"Paris"},
		},
		{
			name:     "局部搜索评估 (Local)",
			query:    "What is SQLiteAI?",
			mode:     ModeLocal,
			expected: []string{"SQLiteAI", "Golang"}, // Local 应该通过实体关联找到相关文档
		},
		{
			name:     "混合搜索评估 (Hybrid)",
			query:    "Golang database",
			mode:     ModeHybrid,
			expected: []string{"SQLiteAI", "Golang"},
		},
		{
			name:     "朴素搜索评估 (Naive)",
			query:    "The Eiffel Tower",
			mode:     ModeNaive,
			expected: []string{"Eiffel Tower"},
		},
		{
			name:     "混合模式评估 (Mix)",
			query:    "What is SQLiteAI?",
			mode:     ModeMix,
			expected: []string{"SQLiteAI", "Golang"}, // Mix 模式结合图谱和向量检索
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := rag.Retrieve(ctx, tt.query, QueryParam{Mode: tt.mode, Limit: 2})
			if err != nil {
				t.Fatalf("Retrieve failed: %v", err)
			}

			foundCount := 0
			for _, res := range results {
				for _, exp := range tt.expected {
					if strings.Contains(res.Content, exp) {
						foundCount++
						break
					}
				}
			}

			if foundCount == 0 {
				t.Errorf("expected results containing %v, but none found in %d results", tt.expected, len(results))
				for i, res := range results {
					t.Logf("Result %d: %s", i, res.Content)
				}
			} else {
				t.Logf("Mode %s successfully recalled %d expected terms", tt.mode, foundCount)
			}
		})
	}
}

func TestEvaluateAdvancedModes(t *testing.T) {
	workingDir := "./test_modes_eval_advanced"
	rag, cleanup := setupRAG(t, workingDir)
	defer cleanup()

	ctx := context.Background()

	// 插入具有关联性的文档
	// SimpleLLM 会提取 "SQLiteAI" 并建立与 "Golang" 的关系
	rag.Insert(ctx, "SQLiteAI is a database built for Golang.")
	// 等待提取
	time.Sleep(1 * time.Second)

	// ModeGraph 应该找到实体和关系
	t.Run("图搜索评估 (Graph Mode)", func(t *testing.T) {
		results, err := rag.Retrieve(ctx, "Tell me about SQLiteAI architecture", QueryParam{Mode: ModeGraph, Limit: 2})
		if err != nil {
			t.Fatalf("Graph mode failed: %v", err)
		}

		if len(results) == 0 {
			t.Error("Graph mode returned no results")
		}

		hasSQLiteAI := false
		for _, res := range results {
			if strings.Contains(res.Content, "SQLiteAI") {
				hasSQLiteAI = true
			}
			// 检查是否召回了三元组
			if len(res.RecalledTriples) > 0 {
				t.Logf("Recalled %d triples in Graph mode", len(res.RecalledTriples))
				for _, tri := range res.RecalledTriples {
					t.Logf("Triple: %s -[%s]-> %s", tri.Source, tri.Relation, tri.Target)
				}
			}
		}
		if !hasSQLiteAI {
			t.Error("Graph mode failed to recall doc containing SQLiteAI")
		}
	})

	t.Run("全局搜索评估 (Global Mode)", func(t *testing.T) {
		// Global 模式使用 high-level keywords，查询与数据库相关的主题
		// 插入的文档包含 "database"，所以查询 "database system" 应该能找到相关文档
		results, err := rag.Retrieve(ctx, "database system", QueryParam{Mode: ModeGlobal, Limit: 2})
		if err != nil {
			t.Fatalf("Global mode failed: %v", err)
		}

		if len(results) == 0 {
			t.Error("Global mode returned no results")
		}

		hasTriples := false
		for _, res := range results {
			if len(res.RecalledTriples) > 0 {
				hasTriples = true
				break
			}
		}
		if !hasTriples {
			t.Log("Note: Global mode recalled no triples in this test case (depends on entity extraction)")
		}
	})

	t.Run("混合模式评估 (Mix Mode)", func(t *testing.T) {
		// Mix 模式结合知识图谱和向量检索
		results, err := rag.Retrieve(ctx, "SQLiteAI database", QueryParam{Mode: ModeMix, Limit: 2})
		if err != nil {
			t.Fatalf("Mix mode failed: %v", err)
		}

		if len(results) == 0 {
			t.Error("Mix mode returned no results")
		}

		hasSQLiteAI := false
		for _, res := range results {
			if strings.Contains(res.Content, "SQLiteAI") {
				hasSQLiteAI = true
			}
			// Mix 模式应该召回三元组（来自图谱检索）
			if len(res.RecalledTriples) > 0 {
				t.Logf("Mix mode recalled %d triples", len(res.RecalledTriples))
			}
		}
		if !hasSQLiteAI {
			t.Error("Mix mode failed to recall doc containing SQLiteAI")
		}
	})
}

func TestHybridSearchRRF(t *testing.T) {
	workingDir := "./test_modes_eval_rrf"
	rag, cleanup := setupRAG(t, workingDir)
	defer cleanup()

	ctx := context.Background()

	// 插入一些文档，使得向量搜索和全文搜索有不同的侧重点
	rag.Insert(ctx, "Apple is a fruit.")                   // 文档 A
	rag.Insert(ctx, "Apple Inc. is a technology company.") // 文档 B

	time.Sleep(1 * time.Second)

	// 查询 "Apple fruit"
	// 全文搜索应该能匹配到 "Apple" 和 "fruit" -> 文档 A 排名高
	// 向量搜索 "Apple fruit" 可能也会倾向于文档 A

	results, err := rag.Retrieve(ctx, "Apple fruit", QueryParam{Mode: ModeHybrid, Limit: 2})
	if err != nil {
		t.Fatalf("Hybrid search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("No results found")
	}

	t.Logf("First result in Hybrid: %s (Score: %f)", results[0].Content, results[0].Score)
	if !strings.Contains(results[0].Content, "fruit") {
		t.Errorf("Expected 'Apple is a fruit' to be the top result, got: %s", results[0].Content)
	}
}
