package main

import (
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
	_ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
	"github.com/sirupsen/logrus"
)

func main() {
	// 预加载 sego 词典
	if err := sego.Init(); err != nil {
		logrus.WithError(err).Warn("Failed to initialize sego dictionary")
	}

	// 初始化数据库
	if err := initDatabase(); err != nil {
		logrus.WithError(err).Fatal("Failed to initialize database")
	}
	defer sqlDB.Close()
	if graphDB != nil {
		defer graphDB.Close()
	}

	// 设置 Gin 路由
	r := gin.Default()

	// 配置 CORS
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	r.Use(cors.New(config))

	// API 路由
	api := r.Group("/api")
	{
		// 数据库信息
		api.GET("/db/info", getDBInfo)
		api.GET("/db/collections", getCollections)

		// 集合操作
		api.GET("/collections/:name", getCollection)
		api.GET("/collections/:name/documents", getDocuments)
		api.GET("/collections/:name/documents/:id", getDocument)
		api.POST("/collections/:name/documents", createDocument)
		api.PUT("/collections/:name/documents/:id", updateDocument)
		api.DELETE("/collections/:name/documents/:id", deleteDocument)

		// 全文搜索
		api.POST("/collections/:name/fulltext/search", fulltextSearch)

		// 向量搜索
		api.POST("/collections/:name/vector/search", vectorSearch)

		// 图数据库操作
		api.POST("/graph/link", graphLink)
		api.DELETE("/graph/link", graphUnlink)
		api.GET("/graph/neighbors/:nodeId", graphNeighbors)
		api.POST("/graph/path", graphPath)
		api.POST("/graph/query", graphQuery)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "40121"
	}

	logrus.WithField("port", port).Info("Server starting")
	if err := r.Run(":" + port); err != nil {
		logrus.WithError(err).Fatal("Failed to start server")
	}
}
