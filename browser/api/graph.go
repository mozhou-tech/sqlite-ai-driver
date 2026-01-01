package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// graphLink 创建图关系链接
func graphLink(c *gin.Context) {
	var req GraphLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	if err := graphDB.Link(dbContext, req.From, req.Relation, req.To); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Link created successfully",
		"from":     req.From,
		"relation": req.Relation,
		"to":       req.To,
	})
}

// graphUnlink 删除图关系链接
func graphUnlink(c *gin.Context) {
	var req GraphLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	if err := graphDB.Unlink(dbContext, req.From, req.Relation, req.To); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Link deleted successfully",
		"from":     req.From,
		"relation": req.Relation,
		"to":       req.To,
	})
}

// graphNeighbors 获取节点的邻居
func graphNeighbors(c *gin.Context) {
	nodeID := c.Param("nodeId")
	relation := c.DefaultQuery("relation", "")

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	neighbors, err := graphDB.GetNeighbors(dbContext, nodeID, relation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"node_id":   nodeID,
		"relation":  relation,
		"neighbors": neighbors,
	})
}

// graphPath 查找两个节点之间的路径
func graphPath(c *gin.Context) {
	var req GraphPathRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if req.MaxDepth == 0 {
		req.MaxDepth = 5
	}

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	var paths [][]string
	var err error

	predicate := ""
	if len(req.Relations) > 0 {
		predicate = req.Relations[0]
	}

	paths, err = graphDB.FindPath(dbContext, req.From, req.To, req.MaxDepth, predicate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"from":  req.From,
		"to":    req.To,
		"paths": paths,
	})
}

// graphQuery 执行图查询
func graphQuery(c *gin.Context) {
	var req GraphQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if graphDB == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Graph database not available",
		})
		return
	}

	query := graphDB.Query()
	if query == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Query builder not available",
		})
		return
	}

	logrus.WithField("query", req.Query).Info("🔍 解析查询字符串")

	if !strings.HasPrefix(req.Query, "V(") {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "查询必须以 V('nodeId') 开始",
		})
		return
	}

	var nodeID string
	var vEndIndex int

	nodeStart := strings.Index(req.Query, "('")
	if nodeStart == -1 {
		nodeStart = strings.Index(req.Query, "(\"")
		if nodeStart == -1 {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error: "无法解析节点ID，格式应为 V('nodeId') 或 V(\"nodeId\")",
			})
			return
		}
		relEnd := strings.Index(req.Query[nodeStart+2:], "\")")
		if relEnd == -1 {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "节点ID格式错误"})
			return
		}
		nodeID = req.Query[nodeStart+2 : nodeStart+2+relEnd]
		vEndIndex = nodeStart + 2 + relEnd + 2
	} else {
		relEnd := strings.Index(req.Query[nodeStart+2:], "')")
		if relEnd == -1 {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "节点ID格式错误"})
			return
		}
		nodeID = req.Query[nodeStart+2 : nodeStart+2+relEnd]
		vEndIndex = nodeStart + 2 + relEnd + 2
	}

	logrus.WithField("node_id", nodeID).Info("📌 提取节点ID")

	queryImpl := query.V(nodeID)
	if queryImpl == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "创建查询失败",
		})
		return
	}

	remainingQuery := ""
	if vEndIndex < len(req.Query) {
		remainingQuery = req.Query[vEndIndex:]
	}
	logrus.WithField("remaining_query", remainingQuery).Info("📋 剩余查询部分")

	if strings.HasPrefix(remainingQuery, ".Out(") {
		relStart := strings.Index(remainingQuery, "('")
		if relStart == -1 {
			relStart = strings.Index(remainingQuery, "(\"")
			if relStart == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无法解析关系名称"})
				return
			}
			relEnd := strings.Index(remainingQuery[relStart+2:], "\")")
			if relEnd == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "关系名称格式错误"})
				return
			}
			relation := remainingQuery[relStart+2 : relStart+2+relEnd]
			logrus.WithField("relation", relation).Info("🔗 提取关系 (Out)")
			queryImpl = queryImpl.Out(relation)
		} else {
			relEnd := strings.Index(remainingQuery[relStart+2:], "')")
			if relEnd == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "关系名称格式错误"})
				return
			}
			relation := remainingQuery[relStart+2 : relStart+2+relEnd]
			logrus.WithField("relation", relation).Info("🔗 提取关系 (Out)")
			queryImpl = queryImpl.Out(relation)
		}
	} else if strings.HasPrefix(remainingQuery, ".In(") {
		relStart := strings.Index(remainingQuery, "('")
		if relStart == -1 {
			relStart = strings.Index(remainingQuery, "(\"")
			if relStart == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无法解析关系名称"})
				return
			}
			relEnd := strings.Index(remainingQuery[relStart+2:], "\")")
			if relEnd == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "关系名称格式错误"})
				return
			}
			relation := remainingQuery[relStart+2 : relStart+2+relEnd]
			logrus.WithField("relation", relation).Info("🔗 提取关系 (In)")
			queryImpl = queryImpl.In(relation)
		} else {
			relEnd := strings.Index(remainingQuery[relStart+2:], "')")
			if relEnd == -1 {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "关系名称格式错误"})
				return
			}
			relation := remainingQuery[relStart+2 : relStart+2+relEnd]
			logrus.WithField("relation", relation).Info("🔗 提取关系 (In)")
			queryImpl = queryImpl.In(relation)
		}
	}

	if queryImpl == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "构建查询失败",
		})
		return
	}

	logrus.Info("🚀 执行图查询...")
	queryResults, err := queryImpl.All(dbContext)
	if err != nil {
		logrus.WithError(err).Info("❌ 查询执行失败")
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	logrus.WithField("count", len(queryResults)).Info("✅ 查询成功，找到结果")

	results := make([]gin.H, len(queryResults))
	for i, r := range queryResults {
		results[i] = gin.H{
			"subject":   r.Subject,
			"predicate": r.Predicate,
			"object":    r.Object,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"query":   req.Query,
		"results": results,
	})
}
