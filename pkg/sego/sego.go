package sego

import (
	_ "embed"
	"os"
	"strings"
	"sync"

	huichensego "github.com/huichen/sego"
)

//go:embed dictionary/dictionary.txt
var dictionaryData []byte

var (
	globalSegmenter huichensego.Segmenter
	once            sync.Once
	initErr         error
)

// GetSegmenter 返回全局 sego 分词器，并在需要时初始化。
// 它会自动处理内嵌词典的加载和临时文件的管理。
func GetSegmenter() (*huichensego.Segmenter, error) {
	once.Do(func() {
		tmpFile, err := os.CreateTemp("", "sego-dict-*.txt")
		if err != nil {
			initErr = err
			return
		}
		// 词典加载完后即可删除临时文件
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write(dictionaryData); err != nil {
			initErr = err
			return
		}

		if err := tmpFile.Close(); err != nil {
			initErr = err
			return
		}

		globalSegmenter.LoadDictionary(tmpFile.Name())
	})
	return &globalSegmenter, initErr
}

// Init 显式初始化分词器，用于在程序启动时预加载词典
func Init() error {
	_, err := GetSegmenter()
	return err
}

// Tokenize 使用 sego 对文本进行中文分词，返回用空格分隔的词
func Tokenize(text string) string {
	if text == "" {
		return ""
	}

	segmenter, err := GetSegmenter()
	if err != nil {
		return text
	}

	segments := segmenter.Segment([]byte(text))
	var tokens []string
	for _, seg := range segments {
		token := seg.Token().Text()
		// 过滤掉空白字符和标点符号
		token = strings.TrimSpace(token)
		if token != "" {
			tokens = append(tokens, token)
		}
	}

	if len(tokens) == 0 {
		return text // 如果分词结果为空，返回原文
	}

	return strings.Join(tokens, " ")
}
