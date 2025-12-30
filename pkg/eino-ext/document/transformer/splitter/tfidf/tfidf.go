/*
 * Copyright 2024 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0

 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package tfidf

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/sego"
	"github.com/rioloc/tfidf-go"
	"github.com/rioloc/tfidf-go/token"
)

var (
	globalSegoErr error
)

// SetSegoDict allows setting the dictionary data for sego tokenizer.
// Deprecated: now uses pkg/sego embedded dictionary.
func SetSegoDict(dict []byte) {
}

// IDGenerator generates new IDs for split chunks
type IDGenerator func(ctx context.Context, originalID string, splitIndex int) string

// defaultIDGenerator keeps the original ID
func defaultIDGenerator(ctx context.Context, originalID string, _ int) string {
	return originalID
}

type Config struct {
	// SimilarityThreshold is the minimum cosine similarity between sentences to keep them in the same chunk.
	// If similarity is below this, a new chunk is started.
	// Default is 0.2.
	SimilarityThreshold float64
	// MaxChunkSize is the maximum number of characters (runes) in a chunk.
	// When a chunk reaches this size, it will be split regardless of similarity.
	// Default is 1000.
	MaxChunkSize int
	// MinChunkSize is the minimum number of characters (runes) in a chunk.
	// If a chunk is shorter than this, more sentences will be added even if similarity is low.
	// Default is 50.
	MinChunkSize int
	// MaxSentencesPerChunk is the maximum number of sentences in a chunk.
	// This is a secondary limit to prevent chunks with too many sentences.
	// Default is 50.
	MaxSentencesPerChunk int
	// RemoveWhitespace specifies whether to remove all whitespace characters from the final chunks.
	// Default is false.
	RemoveWhitespace bool
	// UseSego specifies whether to use sego for Chinese tokenization.
	UseSego bool
	// SegoDictPath is the path to the sego dictionary file.
	// If empty and UseSego is true, it will try to use globalSegoDict or a default path.
	SegoDictPath string
	// IDGenerator is an optional function to generate new IDs for split chunks.
	// If nil, the original document ID will be used for all splits.
	IDGenerator IDGenerator
	// FilterGarbageChunks specifies whether to filter out garbage chunks (like corrupted text from PDF parsing).
	// Defaults to true. Set to false to disable filtering.
	FilterGarbageChunks bool
}

func NewTFIDFSplitter(ctx context.Context, config *Config) (document.Transformer, error) {
	wasNil := config == nil
	if config == nil {
		config = &Config{}
	}
	if config.MaxChunkSize <= 0 {
		config.MaxChunkSize = 1000 // 默认最大 1000 字符
	}
	if config.MinChunkSize <= 0 {
		config.MinChunkSize = 50 // 默认最小 50 字符
	}
	if config.MaxChunkSize < config.MinChunkSize {
		config.MaxChunkSize = config.MinChunkSize
	}
	if config.MaxSentencesPerChunk <= 0 {
		config.MaxSentencesPerChunk = 50 // 默认最多 50 个句子
	}
	if config.SimilarityThreshold <= 0 {
		config.SimilarityThreshold = 0.2
	}
	// FilterGarbageChunks defaults to true
	// Since bool zero value is false, we can't distinguish "unset" from "explicitly false"
	// We default to true when config was nil (user didn't provide config)
	// If user provided config with FilterGarbageChunks: false, we respect that
	if wasNil {
		config.FilterGarbageChunks = true
	}
	// Note: If user creates Config{} without setting FilterGarbageChunks, it will be false (zero value)
	// In that case, filtering will be disabled. To enable, user must explicitly set FilterGarbageChunks: true
	idGenerator := config.IDGenerator
	if idGenerator == nil {
		idGenerator = defaultIDGenerator
	}
	return &tfidfSplitter{
		config:      config,
		idGenerator: idGenerator,
	}, nil
}

type tfidfSplitter struct {
	config      *Config
	idGenerator IDGenerator
}

func (s *tfidfSplitter) Transform(ctx context.Context, docs []*schema.Document, opts ...document.TransformerOption) ([]*schema.Document, error) {
	if s == nil {
		return nil, fmt.Errorf("tfidfSplitter is nil")
	}
	if s.config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	ret := make([]*schema.Document, 0) // 初始化为空切片而不是 nil
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		// 调试：显示原始内容
		runeCount := utf8.RuneCountInString(doc.Content)
		fmt.Printf("\n[DEBUG] 处理文档 %s，原始内容长度: %d 字符 (Runes)\n", doc.ID, runeCount)
		if runeCount > 0 && runeCount <= 200 {
			fmt.Printf("[DEBUG] 原始内容预览: %q\n", doc.Content)
		} else if runeCount > 200 {
			// 安全地截取前 200 个 rune
			runes := []rune(doc.Content)
			fmt.Printf("[DEBUG] 原始内容预览（前200字符）: %q\n", string(runes[:200]))
		}

		chunks, err := s.splitText(doc.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to split document %s: %w", doc.ID, err)
		}
		fmt.Printf("[DEBUG] splitText 返回 %d 个 chunks\n", len(chunks))

		// 如果 splitText 返回 nil 或空切片，至少保留原始文档
		if len(chunks) == 0 {
			// 如果内容为空，跳过该文档
			if doc.Content == "" {
				fmt.Printf("\n========== 文档 %s (跳过，内容为空) ==========\n\n", doc.ID)
				continue
			}
			// 否则创建一个包含原始内容的文档
			chunkID := s.idGenerator(ctx, doc.ID, 0)
			fmt.Printf("\n========== 文档 %s 分割为 1 个 chunk (未分割) ==========\n", doc.ID)
			fmt.Printf("\n--- Chunk 1 (ID: %s) ---\n", chunkID)
			fmt.Printf("长度: %d 字符 (Runes)\n", runeCount)
			fmt.Printf("内容:\n%s\n", doc.Content)
			fmt.Printf("---\n")
			fmt.Printf("==========================================\n\n")

			nDoc := &schema.Document{
				ID:       chunkID,
				Content:  doc.Content,
				MetaData: deepCopyAnyMap(doc.MetaData),
			}
			ret = append(ret, nDoc)
		} else {
			fmt.Printf("\n========== 文档 %s 分割为 %d 个 chunks ==========\n", doc.ID, len(chunks))
			for i, chunk := range chunks {
				chunkID := s.idGenerator(ctx, doc.ID, i)
				cRuneCount := utf8.RuneCountInString(chunk)
				fmt.Printf("\n--- Chunk %d (ID: %s) ---\n", i+1, chunkID)
				fmt.Printf("长度: %d 字符 (Runes)\n", cRuneCount)
				fmt.Printf("内容:\n%s\n", chunk)
				fmt.Printf("---\n")

				nDoc := &schema.Document{
					ID:       chunkID,
					Content:  chunk,
					MetaData: deepCopyAnyMap(doc.MetaData),
				}
				ret = append(ret, nDoc)
			}
			fmt.Printf("==========================================\n\n")
		}
	}
	return ret, nil
}

func (s *tfidfSplitter) GetType() string {
	return "TFIDFSplitter"
}

func (s *tfidfSplitter) splitText(text string) ([]string, error) {
	// 安全检查
	if s == nil || s.config == nil {
		return []string{text}, nil
	}

	// 如果文本为空，返回空切片
	if text == "" {
		return []string{}, nil
	}

	// 如果文本太短（少于 MinChunkSize 个字符），直接返回原始文本，不进行分割
	trimmed := strings.TrimSpace(text)
	if utf8.RuneCountInString(trimmed) < s.config.MinChunkSize {
		if trimmed == "" {
			return []string{}, nil
		}
		// 如果包含表格，使用 cleanChunk 保留表格格式
		if containsTable(trimmed) {
			chunk := s.cleanChunk(trimmed)
			return []string{chunk}, nil
		}
		// 不包含表格，按原来的逻辑处理
		chunk := trimmed
		if s.config.RemoveWhitespace {
			chunk = removeAllWhitespace(chunk)
		} else {
			chunk = strings.ReplaceAll(chunk, "\n", " ")
			chunk = strings.ReplaceAll(chunk, "\r", " ")
		}
		return []string{chunk}, nil
	}

	// 1. Split into sentences
	sentences := splitIntoSentences(text)
	if len(sentences) == 0 {
		// 如果无法分割成句子，但文本不为空，返回包含原始文本的切片
		if trimmed != "" {
			return []string{trimmed}, nil
		}
		return []string{}, nil
	}
	if len(sentences) == 1 {
		return sentences, nil
	}

	// 过滤掉太短的句子（少于3个字符），避免产生只有1-2个字符的chunk
	filteredSentences := make([]string, 0, len(sentences))
	for _, sent := range sentences {
		trimmedSent := strings.TrimSpace(sent)
		if utf8.RuneCountInString(trimmedSent) >= 3 {
			filteredSentences = append(filteredSentences, sent)
		}
	}

	// 如果过滤后没有句子，返回原始文本
	if len(filteredSentences) == 0 {
		return []string{trimmed}, nil
	}

	// 如果过滤后只剩一个句子，直接返回
	if len(filteredSentences) == 1 {
		return filteredSentences, nil
	}

	sentences = filteredSentences

	// 2. Calculate TF-IDF for each sentence
	var tfidfMatrix [][]float64

	var vocabulary []string
	var tokens [][]string
	var err error

	if s.config.UseSego {
		vocabulary, tokens, err = s.segoTokenize(sentences)
		if err != nil {
			return nil, fmt.Errorf("sego tokenizer failed: %w", err)
		}
	} else {
		tokenizer := token.NewTokenizer()
		if tokenizer != nil {
			vocabulary, tokens, err = tokenizer.Tokenize(sentences)
		} else {
			err = fmt.Errorf("tokenizer creation failed")
		}
	}

	if err == nil && vocabulary != nil && tokens != nil && len(tokens) == len(sentences) {
		tfMatrix := tfidf.Tf(vocabulary, tokens)
		idfVector := tfidf.Idf(vocabulary, tokens, true)
		vectorizer := tfidf.NewTfIdfVectorizer()
		if tfMatrix != nil && idfVector != nil && vectorizer != nil {
			tfidfMatrix, _ = vectorizer.TfIdf(tfMatrix, idfVector)
		}
	}

	// 3. Group sentences into chunks
	return s.groupSentences(sentences, tfidfMatrix), nil
}

func (s *tfidfSplitter) groupSentences(sentences []string, tfidfMatrix [][]float64) []string {
	if len(sentences) == 0 {
		return []string{}
	}

	var chunks []string
	var currentChunk []string
	var currentLength int
	var inTable bool // 标记当前是否在处理表格

	joinSep := " "
	if s.config.RemoveWhitespace {
		joinSep = ""
	}

	for i := 0; i < len(sentences); i++ {
		sentence := sentences[i]
		isTableRow := isMarkdownTableRow(sentence)

		// 检测表格开始
		if isTableRow && !inTable {
			// 如果当前 chunk 不为空，先保存它
			if len(currentChunk) > 0 {
				chunk := s.cleanChunk(strings.Join(currentChunk, joinSep))
				chunks = append(chunks, chunk)
				currentChunk = []string{}
				currentLength = 0
			}
			inTable = true
		}

		// 检测表格结束
		if !isTableRow && inTable {
			// 表格结束，检查是否需要分割
			if len(currentChunk) > 0 {
				// 硬性限制：embedding API 通常限制在 8192 字符，我们设置为 8000 以留出余量
				const maxEmbeddingSize = 8000
				tableChunk := strings.Join(currentChunk, "\n")
				chunkLen := utf8.RuneCountInString(tableChunk)

				if chunkLen > maxEmbeddingSize {
					// 表格超过硬性限制，需要按行分割
					tableChunks := s.splitLargeTable(currentChunk, maxEmbeddingSize)
					chunks = append(chunks, tableChunks...)
				} else {
					// 表格内容保留换行符，使用 "\n" 连接
					chunks = append(chunks, tableChunk)
				}
				currentChunk = []string{}
				currentLength = 0
			}
			inTable = false
		}

		// 如果是第一个句子，直接添加
		if len(currentChunk) == 0 {
			currentChunk = append(currentChunk, sentence)
			if inTable {
				// 表格行长度计算（包含换行符）
				currentLength = utf8.RuneCountInString(sentence)
			} else {
				currentLength = utf8.RuneCountInString(sentence)
			}
			continue
		}

		// 如果正在处理表格，检查是否需要分割
		if inTable {
			// 硬性限制：embedding API 通常限制在 8192 字符，我们设置为 8000 以留出余量
			const maxEmbeddingSize = 8000
			potentialLength := currentLength + utf8.RuneCountInString(sentence) + 1 // +1 for newline

			// 如果添加这一行会超过限制，先保存当前 chunk
			if potentialLength > maxEmbeddingSize && len(currentChunk) > 0 {
				tableChunk := strings.Join(currentChunk, "\n")
				chunkLen := utf8.RuneCountInString(tableChunk)
				if chunkLen > maxEmbeddingSize {
					// 当前 chunk 已经超过限制，需要分割
					tableChunks := s.splitLargeTable(currentChunk, maxEmbeddingSize)
					chunks = append(chunks, tableChunks...)
					// 开始新的 chunk
					currentChunk = []string{sentence}
					currentLength = utf8.RuneCountInString(sentence)
				} else {
					chunks = append(chunks, tableChunk)
					currentChunk = []string{sentence}
					currentLength = utf8.RuneCountInString(sentence)
				}
			} else {
				currentChunk = append(currentChunk, sentence)
				currentLength = potentialLength
			}
			continue
		}

		// 计算相似度（如果有 TF-IDF 矩阵）
		sim := 1.0 // 默认高度相似，防止在没有矩阵时乱切分
		if tfidfMatrix != nil && i > 0 && i < len(tfidfMatrix) && i-1 < len(tfidfMatrix) {
			if tfidfMatrix[i-1] != nil && tfidfMatrix[i] != nil {
				sim = cosineSimilarity(tfidfMatrix[i-1], tfidfMatrix[i])
			}
		}

		// 识别当前句子是否为 Markdown 标题
		isHeader := isMarkdownHeader(strings.TrimSpace(sentence))

		// 切分判定
		shouldSplit := isHeader || sim < s.config.SimilarityThreshold || currentLength >= s.config.MaxChunkSize || len(currentChunk) >= s.config.MaxSentencesPerChunk
		canSplit := currentLength >= s.config.MinChunkSize
		forceSplit := currentLength >= s.config.MaxChunkSize || len(currentChunk) >= s.config.MaxSentencesPerChunk*2

		if (shouldSplit && canSplit) || forceSplit {
			chunk := s.cleanChunk(strings.Join(currentChunk, joinSep))
			chunks = append(chunks, chunk)
			currentChunk = []string{sentence}
			currentLength = utf8.RuneCountInString(sentence)
		} else {
			currentChunk = append(currentChunk, sentence)
			currentLength += utf8.RuneCountInString(sentence) + utf8.RuneCountInString(joinSep)
		}
	}

	// 处理最后一个 chunk
	if len(currentChunk) > 0 {
		var chunk string
		if inTable {
			// 表格内容保留换行符
			chunk = strings.Join(currentChunk, "\n")
		} else {
			chunk = s.cleanChunk(strings.Join(currentChunk, joinSep))
		}
		chunkLen := utf8.RuneCountInString(chunk)

		// 硬性限制：embedding API 通常限制在 8192 字符，我们设置为 8000 以留出余量
		const maxEmbeddingSize = 8000

		if inTable && chunkLen > maxEmbeddingSize {
			// 表格超过硬性限制，需要按行分割，但保持表格行完整
			tableChunks := s.splitLargeTable(currentChunk, maxEmbeddingSize)
			chunks = append(chunks, tableChunks...)
		} else if !inTable && chunkLen < s.config.MinChunkSize && len(chunks) > 0 {
			// 对于普通文本，强制合并最后一个 Chunk，只要它小于 MinChunkSize 且前面还有 Chunk
			prevChunk := chunks[len(chunks)-1]
			mergedChunk := prevChunk + joinSep + chunk
			// 放宽限制：为了满足 MinChunkSize，允许合并后的结果超过 MaxChunkSize
			// 但我们仍然保留一个合理的上限，比如 MaxChunkSize * 3，但不能超过 embedding 限制
			mergedLen := utf8.RuneCountInString(mergedChunk)
			if mergedLen <= s.config.MaxChunkSize*3 && mergedLen <= maxEmbeddingSize {
				chunks[len(chunks)-1] = mergedChunk
			} else {
				chunks = append(chunks, chunk)
			}
		} else {
			chunks = append(chunks, chunk)
		}
	}

	// Filter garbage chunks if enabled
	if s.config.FilterGarbageChunks {
		filteredChunks := make([]string, 0, len(chunks))
		for i, chunk := range chunks {
			if isGarbageChunk(chunk) {
				chunkLen := utf8.RuneCountInString(chunk)
				// 截取前100个字符用于日志显示
				preview := chunk
				if chunkLen > 100 {
					runes := []rune(chunk)
					preview = string(runes[:100]) + "..."
				}
				fmt.Printf("[DEBUG] 过滤乱码 Chunk %d (长度: %d 字符): %q\n", i+1, chunkLen, preview)
			} else {
				filteredChunks = append(filteredChunks, chunk)
			}
		}
		if len(filteredChunks) < len(chunks) {
			fmt.Printf("[DEBUG] 共过滤 %d 个乱码 chunk，保留 %d 个有效 chunk\n", len(chunks)-len(filteredChunks), len(filteredChunks))
		}
		return filteredChunks
	}

	return chunks
}

// splitLargeTable 将大表格按行分割成多个 chunk，保持表格行完整
// 尽量保持表格的语义完整性：如果可能，在每个 chunk 中包含表头
func (s *tfidfSplitter) splitLargeTable(tableRows []string, maxSize int) []string {
	if len(tableRows) == 0 {
		return nil
	}

	var chunks []string
	var currentChunk []string
	var currentLength int

	// 尝试识别表头（通常是前两行：标题行和分隔行）
	var headerRows []string
	headerEndIndex := 0

	// 查找表头：第一行通常是标题，第二行可能是分隔行（包含 ---）
	if len(tableRows) > 0 && isMarkdownTableRow(tableRows[0]) {
		headerRows = append(headerRows, tableRows[0])
		headerEndIndex = 1
		currentLength = utf8.RuneCountInString(tableRows[0])

		// 检查第二行是否是分隔行
		if len(tableRows) > 1 && isMarkdownTableRow(tableRows[1]) {
			secondRow := tableRows[1]
			if strings.Contains(secondRow, "---") {
				headerRows = append(headerRows, secondRow)
				headerEndIndex = 2
				currentLength += utf8.RuneCountInString(secondRow) + 1 // +1 for newline
			}
		}
	}

	// 计算表头长度
	headerLength := 0
	if len(headerRows) > 0 {
		headerLength = utf8.RuneCountInString(strings.Join(headerRows, "\n"))
	}

	// 从表头之后开始处理数据行
	for i := headerEndIndex; i < len(tableRows); i++ {
		row := tableRows[i]
		rowLength := utf8.RuneCountInString(row)

		// 计算添加这一行后的长度
		// 如果当前 chunk 为空，需要加上表头（如果存在）
		var potentialLength int
		if len(currentChunk) == 0 && len(headerRows) > 0 {
			// 表头 + 新行：headerLength 已经包含了表头行之间的换行符
			// 还需要加上表头和数据行之间的换行符（len(headerRows) 个换行符）
			potentialLength = headerLength + rowLength + len(headerRows)
		} else {
			potentialLength = currentLength + rowLength + 1 // +1 for newline
		}

		// 如果添加这一行会超过限制，且当前 chunk 不为空，先保存当前 chunk
		if potentialLength > maxSize && len(currentChunk) > 0 {
			chunk := strings.Join(currentChunk, "\n")
			chunks = append(chunks, chunk)
			currentChunk = []string{}
			currentLength = 0
			// 重新计算 potentialLength（因为 currentChunk 已清空）
			if len(headerRows) > 0 {
				potentialLength = headerLength + rowLength + len(headerRows)
			} else {
				potentialLength = rowLength
			}
		}

		// 如果当前 chunk 为空，添加表头（如果存在且不会超过限制）
		if len(currentChunk) == 0 {
			if len(headerRows) > 0 {
				// 检查单独一行是否超过限制
				if rowLength > maxSize {
					// 单行就超过限制，无法处理，直接添加（这种情况很少见）
					currentChunk = append(currentChunk, row)
					currentLength = rowLength
				} else if potentialLength <= maxSize {
					// 可以添加表头
					currentChunk = append(currentChunk, headerRows...)
					currentChunk = append(currentChunk, row)
					currentLength = potentialLength
				} else {
					// 表头+行超过限制，只添加行
					currentChunk = append(currentChunk, row)
					currentLength = rowLength
				}
			} else {
				// 没有表头，直接添加行
				currentChunk = append(currentChunk, row)
				currentLength = rowLength
			}
		} else {
			// 添加数据行到现有 chunk
			currentChunk = append(currentChunk, row)
			currentLength = potentialLength
		}
	}

	// 处理最后一个 chunk
	if len(currentChunk) > 0 {
		chunk := strings.Join(currentChunk, "\n")
		chunks = append(chunks, chunk)
	}

	return chunks
}

func (s *tfidfSplitter) cleanChunk(chunk string) string {
	// 检查是否包含表格
	if containsTable(chunk) {
		// 如果包含表格，保留表格格式（包括换行符）
		// 即使 RemoveWhitespace=true，也保留表格的换行符
		// 只对非表格行进行空白处理
		lines := strings.Split(chunk, "\n")
		var result []string
		for _, line := range lines {
			if isMarkdownTableRow(line) {
				// 表格行：保留原样（包括前导和尾随空格）
				result = append(result, line)
			} else {
				// 非表格行：根据配置处理
				if s.config.RemoveWhitespace {
					cleaned := removeAllWhitespace(line)
					if cleaned != "" {
						result = append(result, cleaned)
					}
				} else {
					// 移除回车符，但保留空格和换行符结构
					cleaned := strings.ReplaceAll(line, "\r", "")
					cleaned = strings.TrimSpace(cleaned)
					if cleaned != "" {
						result = append(result, cleaned)
					}
				}
			}
		}
		// 对于包含表格的 chunk，始终使用换行符连接所有行
		// 这样可以保持表格的结构完整性
		return strings.Join(result, "\n")
	}

	// 不包含表格，按原来的逻辑处理
	if s.config.RemoveWhitespace {
		return removeAllWhitespace(chunk)
	}
	// 即使不移除所有空白，也应移除换行符
	chunk = strings.ReplaceAll(chunk, "\n", " ")
	chunk = strings.ReplaceAll(chunk, "\r", " ")
	return chunk
}

func removeAllWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// isGarbageChunk 基于 sego 分词判断一个 chunk 是否是乱码
// 乱码特征：
// 1. 有效词比例过低（有效词比例 < 20%）
// 2. 有效词比例较低且单字符词比例过高（有效词比例 < 30% 且单字符词比例 > 50%）
func isGarbageChunk(chunk string) bool {
	if len(chunk) == 0 {
		return false // 空 chunk 不算乱码
	}

	// 基于 sego 分词判断
	segmenter, err := sego.GetSegmenter()
	if err != nil {
		// 如果 sego 初始化失败，无法判断，返回 false（不认为是乱码）
		return false
	}

	segments := segmenter.Segment([]byte(chunk))
	if len(segments) == 0 {
		// 如果分词结果为空，可能是乱码
		return true
	}

	var (
		validTokens      int // 有效词：长度 >= 2 且包含有效字符的词
		singleCharTokens int // 单字符词
		totalTokens      int // 总词数
	)

	for _, seg := range segments {
		tokenStr := seg.Token().Text()
		tokenStr = strings.TrimSpace(tokenStr)
		if len(tokenStr) == 0 {
			continue
		}
		totalTokens++

		tokenRunes := []rune(tokenStr)
		tokenLen := len(tokenRunes)

		// 单字符词
		if tokenLen == 1 {
			singleCharTokens++
		} else {
			// 多字符词，检查是否包含有效字符
			hasValidChar := false
			for _, r := range tokenRunes {
				if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) || unicode.IsNumber(r) {
					hasValidChar = true
					break
				}
			}
			if hasValidChar {
				validTokens++
			}
		}
	}

	if totalTokens == 0 {
		// 如果分词结果为空，认为是乱码
		return true
	}

	validTokenRatio := float64(validTokens) / float64(totalTokens)
	singleCharRatio := float64(singleCharTokens) / float64(totalTokens)

	// 判断规则：
	// 1. 如果有效词比例 < 20%，直接认为是乱码
	// 2. 如果有效词比例 < 30% 且单字符词比例 > 50%，认为是乱码
	if validTokenRatio < 0.2 {
		return true
	}
	if validTokenRatio < 0.3 && singleCharRatio > 0.5 {
		return true
	}

	return false
}

func (s *tfidfSplitter) initSego() error {
	return nil
}

func (s *tfidfSplitter) segoTokenize(sentences []string) ([]string, [][]string, error) {
	segmenter, err := sego.GetSegmenter()
	if err != nil {
		return nil, nil, err
	}

	var vocabulary []string
	var tokens [][]string
	wordMap := make(map[string]bool)

	for _, sentence := range sentences {
		segments := segmenter.Segment([]byte(sentence))
		var sentenceTokens []string
		for _, seg := range segments {
			tokenStr := seg.Token().Text()
			// 过滤掉空白
			tokenStr = strings.TrimSpace(tokenStr)
			if len(tokenStr) > 0 {
				sentenceTokens = append(sentenceTokens, tokenStr)
				if !wordMap[tokenStr] {
					wordMap[tokenStr] = true
					vocabulary = append(vocabulary, tokenStr)
				}
			}
		}
		tokens = append(tokens, sentenceTokens)
	}

	return vocabulary, tokens, nil
}

// tableRange 表示一个 Markdown 表格的范围（字节位置）
type tableRange struct {
	start int // 表格开始位置（字节索引）
	end   int // 表格结束位置（字节索引）
}

// isMarkdownTableRow 判断一行是否是 Markdown 表格行
// 表格行特征：
// - 以 | 开头（可选前导空格）
// - 包含至少一个 | 分隔符
// - 以 | 结尾（可选尾随空格）
func isMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return false
	}

	// 必须包含 | 字符
	if !strings.Contains(trimmed, "|") {
		return false
	}

	// 检查是否是表格分隔行（包含 --- 或 :--- 等格式）
	// 例如：| --- | --- | 或 |:---|:---:|---:|
	if strings.Contains(trimmed, "---") {
		// 检查是否包含表格分隔符模式
		sepPattern := regexp.MustCompile(`\|[\s:]*[-:]+[\s:]*\|`)
		return sepPattern.MatchString(trimmed)
	}

	// 普通表格行：以 | 开头（去除前导空格后）
	return strings.HasPrefix(trimmed, "|")
}

// findMarkdownTables 识别文本中所有 Markdown 表格的位置范围
// 返回表格范围的切片，按起始位置排序
func findMarkdownTables(text string) []tableRange {
	if text == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	var tables []tableRange
	var currentTableStart int = -1
	var currentPos int = 0

	for i, line := range lines {
		lineStart := currentPos
		lineEnd := currentPos + len(line)

		isTableRow := isMarkdownTableRow(line)

		if isTableRow {
			if currentTableStart == -1 {
				// 开始一个新的表格
				currentTableStart = lineStart
			}
			// 继续当前表格
		} else {
			// 不是表格行
			if currentTableStart != -1 {
				// 结束当前表格（包含换行符）
				// 表格结束于上一行的末尾
				prevLineEnd := lineStart
				if prevLineEnd > 0 && prevLineEnd < len(text) && text[prevLineEnd-1] == '\n' {
					prevLineEnd--
				}
				tables = append(tables, tableRange{
					start: currentTableStart,
					end:   prevLineEnd,
				})
				currentTableStart = -1
			}
		}

		// 移动到下一行（包括换行符）
		currentPos = lineEnd
		if i < len(lines)-1 {
			currentPos++ // 加上换行符
		}
	}

	// 处理文件末尾的表格
	if currentTableStart != -1 {
		tables = append(tables, tableRange{
			start: currentTableStart,
			end:   len(text),
		})
	}

	return tables
}

// isInTableRange 检查给定的字节位置是否在任何一个表格范围内
func isInTableRange(pos int, tables []tableRange) bool {
	for _, tr := range tables {
		if pos >= tr.start && pos < tr.end {
			return true
		}
	}
	return false
}

// containsTable 检查文本是否包含 Markdown 表格
func containsTable(text string) bool {
	tables := findMarkdownTables(text)
	return len(tables) > 0
}

func splitIntoSentences(text string) []string {
	// 安全检查：处理空文本
	if text == "" {
		return nil
	}

	// 先识别所有表格位置
	tables := findMarkdownTables(text)

	// 使用正则表达式匹配常见的标点符号或 Markdown 标题
	// Markdown 标题必须：在行首（或字符串开头），1-6 个 # 后跟空格，然后是标题内容
	// 普通句子分隔符：中英文句号、问号、感叹号、换行符
	// 注意：只匹配真正的 Markdown 标题格式，避免匹配 PDF 乱码中的 # 字符
	// 注意：小数点（前后都是数字的 .）在循环中检查并排除
	sentenceRegexp := regexp.MustCompile(`((?:^|\n)#{1,6}\s+[^\n\r]*|[.!?。！？\n\r][.!?。！？\n\r\s]*)`)

	// 查找所有匹配的分隔符位置
	matches := sentenceRegexp.FindAllStringIndex(text, -1)

	var sentences []string
	lastPos := 0
	for _, match := range matches {
		// 安全检查：确保 match 有足够的元素
		if len(match) < 2 {
			continue
		}
		start, end := match[0], match[1]

		// 安全检查：确保索引在有效范围内
		if start < 0 || end < 0 || start > len(text) || end > len(text) || start > end {
			continue
		}

		// 跳过表格区域内的匹配
		if isInTableRange(start, tables) || isInTableRange(end, tables) {
			// 如果当前匹配在表格内，跳过
			continue
		}

		// 检查是否是小数点（前后都是数字的情况）
		// 如果匹配到的是 . 且前后都是数字，则跳过（因为这是小数点，不是句子分隔符）
		if start > 0 && end > start && end < len(text) {
			firstChar := text[start]
			if firstChar == '.' {
				// 检查前一个字符是否是数字
				prevRune, _ := utf8.DecodeLastRuneInString(text[:start])
				if unicode.IsDigit(prevRune) {
					// 检查后一个字符是否是数字
					nextRune, _ := utf8.DecodeRuneInString(text[end:])
					if unicode.IsDigit(nextRune) {
						// 这是小数点（前后都是数字），跳过该匹配
						continue
					}
				}
			}
		}

		// 检查 content 区域是否跨越表格
		// 如果 lastPos 到 start 之间包含表格，需要特殊处理
		contentHasTable := false
		for _, tr := range tables {
			if tr.start >= lastPos && tr.end <= start {
				// 表格完全在 content 区域内
				contentHasTable = true
				// 将表格前的文本作为句子
				if tr.start > lastPos {
					preTableContent := cleanContentText(text[lastPos:tr.start])
					if preTableContent != "" {
						sentences = append(sentences, preTableContent)
					}
				}
				// 将整个表格作为一个句子单元（保留格式）
				tableContent := text[tr.start:tr.end]
				if tableContent != "" {
					sentences = append(sentences, tableContent)
				}
				// 更新 lastPos 到表格之后
				lastPos = tr.end
				break
			}
		}

		if contentHasTable {
			// 已经处理了表格，继续处理当前匹配
			// 重新计算 content（从 lastPos 到 start）
			if lastPos < start {
				content := text[lastPos:start]
				delims := text[start:end]

				trimmedDelims := strings.TrimSpace(delims)
				isHeader := isMarkdownHeader(trimmedDelims)

				if isHeader {
					cleanContent := cleanContentText(content)
					if cleanContent != "" {
						sentences = append(sentences, cleanContent)
					}
					cleanHeader := strings.TrimRightFunc(delims, unicode.IsSpace)
					cleanHeader = strings.TrimLeftFunc(cleanHeader, func(r rune) bool {
						return r == '\n' || r == '\r'
					})
					if cleanHeader != "" {
						sentences = append(sentences, cleanHeader)
					}
				} else {
					cleanContent := cleanContentText(content)
					cleanDelims := ""
					for _, r := range delims {
						if !unicode.IsSpace(r) {
							cleanDelims += string(r)
						}
					}
					if cleanContent == "" && len(sentences) > 0 {
						sentences[len(sentences)-1] += cleanDelims
					} else {
						sentence := cleanContent + cleanDelims
						if sentence != "" {
							sentences = append(sentences, sentence)
						}
					}
				}
			}
			lastPos = end
			continue
		}

		// 1. 获取句子正文部分
		content := text[lastPos:start]
		// 2. 获取分隔符区域（标题或标点）
		delims := text[start:end]

		// 检查是否是 Markdown 标题（必须是 # 后跟空格的格式）
		trimmedDelims := strings.TrimSpace(delims)
		isHeader := isMarkdownHeader(trimmedDelims)

		if isHeader {
			// 如果是标题
			cleanContent := cleanContentText(content)
			if cleanContent != "" {
				sentences = append(sentences, cleanContent)
			}
			// 标题本身作为一个独立的句子
			cleanHeader := strings.TrimRightFunc(delims, unicode.IsSpace)
			cleanHeader = strings.TrimLeftFunc(cleanHeader, func(r rune) bool {
				return r == '\n' || r == '\r'
			})
			if cleanHeader != "" {
				sentences = append(sentences, cleanHeader)
			}
		} else {
			// 如果是普通标点
			cleanContent := cleanContentText(content)
			cleanDelims := ""
			for _, r := range delims {
				if !unicode.IsSpace(r) {
					cleanDelims += string(r)
				}
			}

			// 优化点：如果 cleanContent 为空，说明标点紧跟在标题或上一句标点后
			// 将标点附加到最后一个句子中，避免产生独立的标点句子
			if cleanContent == "" && len(sentences) > 0 {
				sentences[len(sentences)-1] += cleanDelims
			} else {
				sentence := cleanContent + cleanDelims
				if sentence != "" {
					sentences = append(sentences, sentence)
				}
			}
		}
		lastPos = end
	}

	// 处理剩余的文本
	if lastPos < len(text) {
		// 检查剩余文本中是否包含表格
		remainingText := text[lastPos:]
		remainingTables := findMarkdownTables(remainingText)

		if len(remainingTables) > 0 {
			// 调整表格位置（加上 lastPos 偏移）
			for i := range remainingTables {
				remainingTables[i].start += lastPos
				remainingTables[i].end += lastPos
			}

			currentPos := lastPos
			for _, tr := range remainingTables {
				// 添加表格前的文本
				if tr.start > currentPos {
					preTableContent := cleanContentText(text[currentPos:tr.start])
					if preTableContent != "" {
						sentences = append(sentences, preTableContent)
					}
				}
				// 添加表格
				tableContent := text[tr.start:tr.end]
				if tableContent != "" {
					sentences = append(sentences, tableContent)
				}
				currentPos = tr.end
			}
			// 添加最后剩余的文本
			if currentPos < len(text) {
				remaining := cleanContentText(text[currentPos:])
				if remaining != "" {
					sentences = append(sentences, remaining)
				}
			}
		} else {
			remaining := cleanContentText(remainingText)
			if remaining != "" {
				sentences = append(sentences, remaining)
			}
		}
	}

	return sentences
}

// isMarkdownHeader 检查字符串是否是有效的 Markdown 标题格式
// 有效格式：1-6 个 # 号后跟至少一个空格，然后是标题内容
func isMarkdownHeader(s string) bool {
	if s == "" {
		return false
	}
	// 统计开头的 # 数量
	hashCount := 0
	for _, r := range s {
		if r == '#' {
			hashCount++
		} else {
			break
		}
	}
	// 必须是 1-6 个 #
	if hashCount < 1 || hashCount > 6 {
		return false
	}
	// # 后面必须跟空格（标准 Markdown 格式）
	if len(s) <= hashCount {
		return false
	}
	// 检查 # 后面是否是空格
	nextChar := s[hashCount]
	return nextChar == ' ' || nextChar == '\t'
}

func cleanContentText(content string) string {
	if content == "" {
		return ""
	}
	// 移除各种特殊的不可见字符
	content = strings.ReplaceAll(content, "\n", "")
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.ReplaceAll(content, "\t", "")
	content = strings.ReplaceAll(content, "\f", "")
	content = strings.ReplaceAll(content, "\v", "")
	content = strings.ReplaceAll(content, "\u0000", "")
	content = strings.ReplaceAll(content, "\u000a", "")
	content = strings.ReplaceAll(content, "\u000d", "")
	content = strings.ReplaceAll(content, "\u0085", "")
	content = strings.ReplaceAll(content, "\u2028", "")
	content = strings.ReplaceAll(content, "\u2029", "")
	// 将多个空格合并为一个
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
	return strings.TrimSpace(content)
}

func cosineSimilarity(v1, v2 []float64) float64 {
	// 安全检查：防止 nil 指针
	if v1 == nil || v2 == nil {
		return 0
	}
	if len(v1) != len(v2) || len(v1) == 0 {
		return 0
	}

	dotProduct := 0.0
	mag1 := 0.0
	mag2 := 0.0
	for i := 0; i < len(v1); i++ {
		dotProduct += v1[i] * v2[i]
		mag1 += v1[i] * v1[i]
		mag2 += v2[i] * v2[i]
	}

	mag1 = math.Sqrt(mag1)
	mag2 = math.Sqrt(mag2)

	if mag1 == 0 || mag2 == 0 {
		return 0
	}

	return dotProduct / (mag1 * mag2)
}

func deepCopyAnyMap(anyMap map[string]any) map[string]any {
	if anyMap == nil {
		return nil
	}
	ret := make(map[string]any)
	for k, v := range anyMap {
		ret[k] = v
	}
	return ret
}
