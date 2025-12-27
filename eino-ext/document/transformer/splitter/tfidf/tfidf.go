/*
 * Copyright 2024 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package tfidf

import (
	"context"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/rxdb-go/pkg/sego"
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
	// MaxChunkSize is the maximum number of sentences in a chunk.
	// Default is 10.
	MaxChunkSize int
	// MinChunkSize is the minimum number of characters in a chunk.
	// If a chunk is shorter than this, more sentences will be added even if similarity is low.
	// Default is 0.
	MinChunkSize int
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
}

func NewTFIDFSplitter(ctx context.Context, config *Config) (document.Transformer, error) {
	if config == nil {
		config = &Config{}
	}
	if config.MaxChunkSize <= 0 {
		config.MaxChunkSize = 10
	}
	if config.SimilarityThreshold <= 0 {
		config.SimilarityThreshold = 0.2
	}
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
	var ret []*schema.Document
	for _, doc := range docs {
		chunks := s.splitText(doc.Content)
		for i, chunk := range chunks {
			nDoc := &schema.Document{
				ID:       s.idGenerator(ctx, doc.ID, i),
				Content:  chunk,
				MetaData: deepCopyAnyMap(doc.MetaData),
			}
			ret = append(ret, nDoc)
		}
	}
	return ret, nil
}

func (s *tfidfSplitter) GetType() string {
	return "TFIDFSplitter"
}

func (s *tfidfSplitter) splitText(text string) []string {
	// 1. Split into sentences
	sentences := splitIntoSentences(text)
	if len(sentences) == 0 {
		return nil
	}
	if len(sentences) == 1 {
		return sentences
	}

	// 2. Calculate TF-IDF for each sentence
	var vocabulary []string
	var tokens [][]string
	var err error

	if s.config.UseSego {
		vocabulary, tokens, err = s.segoTokenize(sentences)
	} else {
		tokenizer := token.NewTokenizer()
		vocabulary, tokens, err = tokenizer.Tokenize(sentences)
	}

	if err != nil {
		// Fallback to simple split if tokenization fails
		return sentences
	}

	tfMatrix := tfidf.Tf(vocabulary, tokens)
	idfVector := tfidf.Idf(vocabulary, tokens, true) // with smoothing

	vectorizer := tfidf.NewTfIdfVectorizer()
	tfidfMatrix, err := vectorizer.TfIdf(tfMatrix, idfVector)
	if err != nil {
		return sentences
	}

	// 3. Group sentences into chunks based on similarity
	var chunks []string
	var currentChunk []string
	var currentLength int

	currentChunk = append(currentChunk, sentences[0])
	currentLength = utf8.RuneCountInString(sentences[0])

	joinSep := " "
	if s.config.RemoveWhitespace {
		joinSep = ""
	}

	for i := 1; i < len(sentences); i++ {
		sim := cosineSimilarity(tfidfMatrix[i-1], tfidfMatrix[i])

		// 识别当前句子是否为 Markdown 标题
		isHeader := strings.HasPrefix(strings.TrimSpace(sentences[i]), "#")

		// 如果是标题，或者相似度低，或者达到最大尺寸，则触发切分意向
		shouldSplit := isHeader || sim < s.config.SimilarityThreshold || len(currentChunk) >= s.config.MaxChunkSize

		// 满足以下切分条件：
		// 1. 必须达到最小长度限制 (MinChunkSize)，除非句子数已经达到限制的两倍以防止无限增长
		// 2. 或者是标题触发且已经达到最小长度
		canSplit := currentLength >= s.config.MinChunkSize
		forceSplit := len(currentChunk) >= s.config.MaxChunkSize*2

		if (shouldSplit && canSplit) || forceSplit {
			chunk := strings.Join(currentChunk, joinSep)
			if s.config.RemoveWhitespace {
				chunk = removeAllWhitespace(chunk)
			} else {
				// 即使不移除所有空白，也应移除换行符
				chunk = strings.ReplaceAll(chunk, "\n", " ")
				chunk = strings.ReplaceAll(chunk, "\r", " ")
			}
			chunks = append(chunks, chunk)
			currentChunk = []string{sentences[i]}
			currentLength = utf8.RuneCountInString(sentences[i])
		} else {
			currentChunk = append(currentChunk, sentences[i])
			currentLength += utf8.RuneCountInString(sentences[i]) + utf8.RuneCountInString(joinSep)
		}
	}

	if len(currentChunk) > 0 {
		chunk := strings.Join(currentChunk, joinSep)
		if s.config.RemoveWhitespace {
			chunk = removeAllWhitespace(chunk)
		} else {
			// 即使不移除所有空白，也应移除换行符，确保返回结果在单行内或符合预期
			chunk = strings.ReplaceAll(chunk, "\n", " ")
			chunk = strings.ReplaceAll(chunk, "\r", " ")
		}
		chunks = append(chunks, chunk)
	}

	return chunks
}

func removeAllWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
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
	vocabMap := make(map[string]bool)
	var tokens [][]string

	for _, sent := range sentences {
		segments := segmenter.Segment([]byte(sent))
		var sentTokens []string
		for _, seg := range segments {
			word := sent[seg.Start():seg.End()]
			word = strings.TrimSpace(word)
			if word == "" {
				continue
			}
			sentTokens = append(sentTokens, word)
			if !vocabMap[word] {
				vocabMap[word] = true
				vocabulary = append(vocabulary, word)
			}
		}
		tokens = append(tokens, sentTokens)
	}
	return vocabulary, tokens, nil
}

func splitIntoSentences(text string) []string {
	// 使用正则表达式匹配常见的标点符号或 Markdown 标题
	// 1. 匹配 1-6 个 # 号开头的标题部分，直到遇到标点或行尾
	// 2. 匹配普通标点符号及其后的空白
	sentenceRegexp := regexp.MustCompile(`(#{1,6}\s*[^#.!?。！？\n\r]*|[.!?。！？\n\r][.!?。！？\n\r\s]*)`)

	// 查找所有匹配的分隔符位置
	matches := sentenceRegexp.FindAllStringIndex(text, -1)

	var sentences []string
	lastPos := 0
	for _, match := range matches {
		start, end := match[0], match[1]

		// 1. 获取句子正文部分
		content := text[lastPos:start]
		// 2. 获取分隔符区域（标题或标点）
		delims := text[start:end]

		// 检查是否是 Markdown 标题
		trimmedDelims := strings.TrimSpace(delims)
		isHeader := strings.HasPrefix(trimmedDelims, "#")

		if isHeader {
			// 如果是标题
			cleanContent := cleanContentText(content)
			if cleanContent != "" {
				sentences = append(sentences, cleanContent)
			}
			// 标题本身作为一个独立的句子
			cleanHeader := strings.TrimRightFunc(delims, unicode.IsSpace)
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
		remaining := cleanContentText(text[lastPos:])
		if remaining != "" {
			sentences = append(sentences, remaining)
		}
	}

	return sentences
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
