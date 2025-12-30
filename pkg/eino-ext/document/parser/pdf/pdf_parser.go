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

package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
	"github.com/ledongthuc/pdf"
)

// Config is the configuration for PDF parser.
type Config struct {
	ToPages bool // whether to
}

// PDFParser reads from io.Reader and parse its content as plain text.
// Attention: This is in alpha stage, and may not support all PDF use cases well enough.
// For example, it will not preserve whitespace and new line for now.
type PDFParser struct {
	ToPages bool
}

// NewPDFParser creates a new PDF parser.
func NewPDFParser(ctx context.Context, config *Config) (*PDFParser, error) {
	if config == nil {
		config = &Config{}
	}
	return &PDFParser{ToPages: config.ToPages}, nil
}

// Parse parses the PDF content from io.Reader.
func (pp *PDFParser) Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) (docs []*schema.Document, err error) {
	commonOpts := parser.GetCommonOptions(nil, opts...)

	specificOpts := parser.GetImplSpecificOptions(&options{
		toPages: &pp.ToPages,
	}, opts...)

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("pdf parser read all from reader failed: %w", err)
	}

	readerAt := bytes.NewReader(data)

	f, err := pdf.NewReader(readerAt, int64(readerAt.Len()))
	if err != nil {
		return nil, fmt.Errorf("create new pdf reader failed: %w", err)
	}

	pages := f.NumPage()
	var (
		buf     bytes.Buffer
		toPages = specificOpts.toPages != nil && *specificOpts.toPages
	)
	fonts := make(map[string]*pdf.Font)
	skippedPages := 0
	for i := 1; i <= pages; i++ {
		p := f.Page(i)
		if p.V.IsNull() { // ledongthuc/pdf.Page is a struct, its internal value V is checked via IsNull()
			fmt.Printf("[PDF Parser] 警告：页面 %d 无效，跳过\n", i)
			skippedPages++
			// 不再创建空文档，直接跳过无效页面
			continue
		}

		// 调试：显示字体信息
		pageFonts := p.Fonts()
		if len(pageFonts) == 0 {
			// 如果没有字体，可能是扫描版PDF（图片），尝试其他方法
			fmt.Printf("[PDF Parser] 页面 %d: 未检测到字体，可能是扫描版PDF\n", i)
		} else {
			fmt.Printf("[PDF Parser] 页面 %d: 检测到 %d 个字体: %v\n", i, len(pageFonts), pageFonts)
		}

		for _, name := range pageFonts { // cache fonts so we don't continually parse charmap
			if _, ok := fonts[name]; !ok {
				font := p.Font(name)
				fonts[name] = &font
			}
		}

		text, err := p.GetPlainText(fonts)
		if err != nil {
			// 跳过有问题的页面，继续处理其他页面
			fmt.Printf("[PDF Parser] 警告：页面 %d 解析失败: %v，跳过此页\n", i, err)
			skippedPages++
			// 不再创建空文档，直接跳过解析失败的页面
			continue
		}

		// 先过滤文本，只保留汉字、英文、数字和中英文标点符号
		filteredText := keepOnlyValidChars(text)
		// 去除文本中所有的空白字符
		cleanedText := removeAllWhitespace(filteredText)

		// 调试：显示提取到的文本信息
		textLength := len(cleanedText)
		fmt.Printf("[PDF Parser] 页面 %d: 原始文本长度=%d, 去除所有空白后长度=%d\n", i, len(text), textLength)

		// 只处理超过100个字符的页面
		minContentLength := 100
		if textLength < minContentLength {
			fmt.Printf("[PDF Parser] 页面 %d: 内容长度 %d < %d，跳过此页\n", i, textLength, minContentLength)
			skippedPages++
			// 不再创建空文档，直接跳过内容过短的页面
			continue
		}

		if textLength > 0 && textLength <= 50 {
			fmt.Printf("[PDF Parser] 页面 %d: 文本预览=%q\n", i, cleanedText[:minInt(textLength, 100)])
		} else if textLength > 100 {
			fmt.Printf("[PDF Parser] 页面 %d: 文本预览（前100字符）=%q\n", i, cleanedText[:100])
		}

		if toPages {
			docs = append(docs, &schema.Document{
				Content:  cleanedText,
				MetaData: commonOpts.ExtraMeta,
			})
		} else {
			// 合并模式：添加页面分隔符，便于后续分割时识别页面边界
			if buf.Len() > 0 {
				buf.WriteString("\n\n--- 页面 " + fmt.Sprintf("%d", i) + " ---\n\n")
			}
			buf.WriteString(cleanedText)
		}
	}

	// 输出统计信息
	if skippedPages > 0 {
		fmt.Printf("[PDF Parser] 完成：总共 %d 页，成功解析 %d 页，跳过 %d 页\n", pages, pages-skippedPages, skippedPages)
	}

	if !toPages {
		docs = append(docs, &schema.Document{
			Content:  buf.String(),
			MetaData: commonOpts.ExtraMeta,
		})
	}

	return docs, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// keepOnlyValidChars 只保留汉字、英文、数字和中英文标点符号
func keepOnlyValidChars(s string) string {
	return strings.Map(func(r rune) rune {
		// 保留汉字
		if unicode.Is(unicode.Han, r) {
			return r
		}
		// 保留英文（ASCII字母）
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		// 保留数字
		if unicode.IsDigit(r) {
			return r
		}
		// 保留中英文标点符号
		if unicode.IsPunct(r) {
			return r
		}
		// 保留空白字符（后续会被 removeAllWhitespace 处理）
		if unicode.IsSpace(r) {
			return r
		}
		// 删除其他字符
		return -1
	}, s)
}

// removeAllWhitespace 移除字符串中的所有空白字符（包括空格、换行、制表符等）
func removeAllWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1 // 删除空白字符
		}
		return r
	}, s)
}
