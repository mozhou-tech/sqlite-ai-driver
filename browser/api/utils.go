package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
)

// tokenizeWithSego 使用 sego 对文本进行分词，返回用空格分隔的词
func tokenizeWithSego(text string) string {
	return sego.Tokenize(text)
}

// extractEmbeddingVector 从 embedding 字段中提取 []float64 向量
func extractEmbeddingVector(embeddingField interface{}) []float64 {
	switch v := embeddingField.(type) {
	case []interface{}:
		result := make([]float64, len(v))
		for i, val := range v {
			if f, ok := val.(float64); ok {
				result[i] = f
			} else if f, ok := val.(float32); ok {
				result[i] = float64(f)
			} else {
				return nil
			}
		}
		return result
	case []float64:
		return v
	case []float32:
		result := make([]float64, len(v))
		for i, val := range v {
			result[i] = float64(val)
		}
		return result
	default:
		return nil
	}
}

// generateID 生成文档 ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// extractTextFromData 从 JSON 数据中提取文本内容
func extractTextFromData(dataJSON string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return ""
	}

	var parts []string
	for k, v := range data {
		if k == "id" || k == "_rev" || k == "embedding" {
			continue
		}
		if str, ok := v.(string); ok {
			parts = append(parts, str)
		} else if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if str, ok := item.(string); ok {
					parts = append(parts, str)
				}
			}
		} else {
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	return strings.Join(parts, " ")
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
