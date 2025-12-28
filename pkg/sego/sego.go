package sego

import (
	_ "embed"
	"os"
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
