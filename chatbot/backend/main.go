package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/gin-gonic/gin"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag"
)

var (
	lightRAGInstance *lightrag.LightRAG
)

func main() {
	// 初始化 LightRAG
	if err := initLightRAG(); err != nil {
		log.Fatalf("Failed to initialize LightRAG: %v", err)
	}

	// 创建 Gin 路由
	r := gin.Default()

	// CORS 中间件
	r.Use(corsMiddleware())

	// API 路由
	api := r.Group("/api")
	{
		api.POST("/chat", handleChat)
		api.POST("/documents", handleAddDocument)
		api.GET("/documents", handleListDocuments)
	}

	// 启动服务器
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func initLightRAG() error {
	ctx := context.Background()

	// 获取环境变量
	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	openaiModel := os.Getenv("OPENAI_MODEL")
	if openaiModel == "" {
		openaiModel = "gpt-4o-mini"
	}

	openaiBaseURL := os.Getenv("OPENAI_BASE_URL")
	if openaiBaseURL == "" {
		openaiBaseURL = "https://api.openai.com/v1"
	}

	// 创建工作目录
	workingDir := os.Getenv("RAG_WORKING_DIR")
	if workingDir == "" {
		workingDir = "./rag_storage"
	}
	if err := os.MkdirAll(workingDir, 0755); err != nil {
		return fmt.Errorf("failed to create working directory: %w", err)
	}

	// 初始化 Embedder
	embedderConfig := &openai.EmbeddingConfig{
		APIKey:  openaiAPIKey,
		BaseURL: openaiBaseURL,
		Model:   "text-embedding-3-small",
	}
	embedderInstance, err := lightrag.NewOpenAIEmbedder(ctx, embedderConfig)
	if err != nil {
		return fmt.Errorf("failed to create embedder: %w", err)
	}

	// 初始化 LLM
	llmConfig := &lightrag.OpenAIConfig{
		APIKey:  openaiAPIKey,
		BaseURL: openaiBaseURL,
		Model:   openaiModel,
	}
	llmInstance := lightrag.NewOpenAILLM(llmConfig)

	// 创建 LightRAG 实例
	lightRAGInstance = lightrag.New(lightrag.Options{
		WorkingDir: workingDir,
		Embedder:   embedderInstance,
		LLM:        llmInstance,
	})

	// 初始化存储
	if err := lightRAGInstance.InitializeStorages(ctx); err != nil {
		return fmt.Errorf("failed to initialize storages: %w", err)
	}

	log.Println("LightRAG initialized successfully")
	return nil
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

type ChatRequest struct {
	Message string   `json:"message"`
	History []string `json:"history,omitempty"`
}

type ChatResponse struct {
	Response string `json:"response"`
}

func handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// 使用 LightRAG 进行查询
	answer, err := lightRAGInstance.Query(ctx, req.Message, lightrag.QueryParam{
		Mode:  lightrag.ModeHybrid,
		Limit: 5,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to query: %v", err)})
		return
	}

	c.JSON(200, ChatResponse{
		Response: answer,
	})
}

type AddDocumentRequest struct {
	Content string `json:"content"`
}

type AddDocumentResponse struct {
	ID string `json:"id"`
}

func handleAddDocument(c *gin.Context) {
	var req AddDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// 插入文档
	if err := lightRAGInstance.Insert(ctx, req.Content); err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to insert document: %v", err)})
		return
	}

	c.JSON(200, AddDocumentResponse{
		ID: "success",
	})
}

func handleListDocuments(c *gin.Context) {
	// 简单实现：返回成功（实际应该从数据库读取）
	c.JSON(200, gin.H{"documents": []string{}})
}
