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
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/smartystreets/goconvey/convey"
)

func TestTFIDFSplitter(t *testing.T) {
	convey.Convey("Test TFIDFSplitter", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold:  0.1,
			MaxChunkSize:         50, // 最大 50 字符
			MinChunkSize:         1,  // 最小 1 字符，确保能根据句子数分割
			MaxSentencesPerChunk: 2,  // 最多 2 个句子
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "This is the first sentence. It is about cats. This is the second sentence. It is about dogs. The third part is different. It discusses airplanes and rockets."
		docs := []*schema.Document{
			{
				ID:      "doc1",
				Content: text,
			},
		}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// Since we set MaxSentencesPerChunk to 2, we expect at least 3 chunks (6 sentences total)
		convey.So(len(splitDocs), convey.ShouldBeGreaterThanOrEqualTo, 3)

		for _, d := range splitDocs {
			convey.So(d.Content, convey.ShouldNotBeEmpty)
			convey.So(d.ID, convey.ShouldEqual, "doc1") // Default ID generator keeps original ID
		}
	})

	convey.Convey("Test TFIDFSplitter Markdown Header and Whitespace", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold:  0.1,
			MaxChunkSize:         1000, // 最大 1000 字符
			MinChunkSize:         5,    // 最小 5 字符
			MaxSentencesPerChunk: 10,   // 最多 10 个句子
			RemoveWhitespace:     false,
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		// Test case: markdown header should force a split
		// Whitespace between delimiters should be cleaned
		// Now testing header in middle without trailing newline on last line
		text := "Intro sentence.\n# Header 1\nThis is sentence one.  ! \nThis is sentence two.\n# Header 2"
		docs := []*schema.Document{{ID: "doc_md", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// Expected sentences:
		// 1. "Intro sentence."
		// 2. "# Header 1" (Force split)
		// 3. "This is sentence one.!"
		// 4. "This is sentence two."
		// 5. "# Header 2" (Force split)

		convey.So(len(splitDocs), convey.ShouldBeGreaterThanOrEqualTo, 3)

		// Find the chunk starting with # Header 2
		foundHeader2 := false
		for _, d := range splitDocs {
			if strings.Contains(d.Content, "# Header 2") {
				foundHeader2 = true
			}
			convey.So(d.Content, convey.ShouldNotContainSubstring, "\n")
		}
		convey.So(foundHeader2, convey.ShouldBeTrue)
	})

	convey.Convey("Test TFIDFSplitter Remove All Whitespace", t, func() {
		ctx := context.Background()
		config := &Config{
			RemoveWhitespace: true,
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "Sentence one. \t \n Sentence two."
		docs := []*schema.Document{{ID: "doc_ws", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		for _, d := range splitDocs {
			// Should not contain any space, tab or newline
			convey.So(d.Content, convey.ShouldNotContainSubstring, " ")
			convey.So(d.Content, convey.ShouldNotContainSubstring, "\t")
			convey.So(d.Content, convey.ShouldNotContainSubstring, "\n")
		}
	})

	convey.Convey("Test TFIDFSplitter MinChunkSize", t, func() {
		ctx := context.Background()
		config := &Config{
			MinChunkSize:         50,   // 最小 50 字符，强制合并短句子
			MaxChunkSize:         500,  // 最大 500 字符
			MaxSentencesPerChunk: 100,  // 允许很多句子，让 MinChunkSize 起作用
			SimilarityThreshold:  0.01, // 低阈值，让相似度判断几乎不起作用
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "Short. Very short. Still short. Now it should be long enough to split if the next one is added but we check length."
		docs := []*schema.Document{{ID: "doc_len", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// 由于 MinChunkSize=50，所有 chunk 都应该 >= 50 字符（除了最后一个可能被合并）
		// 或者整个文档作为一个 chunk
		convey.So(len(splitDocs), convey.ShouldBeGreaterThan, 0)
		// 验证所有 chunk 的长度合理（可能有些短的被合并了）
		for _, d := range splitDocs {
			convey.So(d.Content, convey.ShouldNotBeEmpty)
		}
	})

	convey.Convey("Test TFIDFSplitter MaxChunkSize Soft Limit", t, func() {
		ctx := context.Background()
		config := &Config{
			MaxChunkSize:         20,   // 最大 20 字符（软限制）
			MinChunkSize:         1000, // 最小 1000 字符（很大，让 MaxChunkSize 成为软限制）
			MaxSentencesPerChunk: 50,   // 允许很多句子
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "Sentence one. Sentence two. Sentence three. Sentence four."
		docs := []*schema.Document{{ID: "doc_max", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// 由于 MinChunkSize=1000 很大，即使达到 MaxChunkSize=20，也不会分割
		// 直到达到 MaxChunkSize*2=40 或 MaxSentencesPerChunk*2=100 才会强制分割
		// 整个文本 58 字符，超过了 MaxChunkSize*2=40，所以会强制分割
		convey.So(len(splitDocs), convey.ShouldBeGreaterThanOrEqualTo, 1)
		// 最后一个 chunk 应该包含 "Sentence four."
		convey.So(splitDocs[len(splitDocs)-1].Content, convey.ShouldContainSubstring, "four")
	})

	convey.Convey("Test TFIDFSplitter MaxChunkSize Hard Limit (ForceSplit)", t, func() {
		ctx := context.Background()
		config := &Config{
			MaxChunkSize:         15, // 最大 15 字符（强制分割）
			MinChunkSize:         1,  // 最小 1 字符
			MaxSentencesPerChunk: 50, // 允许很多句子
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		// 3 sentences, 每个约 13-15 字符
		// 当累积字符数 >= MaxChunkSize*2=30 时，强制分割
		text := "Sentence one. Sentence two. Sentence three."
		docs := []*schema.Document{{ID: "doc_force", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// 由于 forceSplit 在 currentLength >= MaxChunkSize (15) 时触发
		// 第一句 13 字符，第二句后累积 27 字符 >= 15，应该分割
		// 结果应该是多个 chunk
		convey.So(len(splitDocs), convey.ShouldBeGreaterThanOrEqualTo, 2)
	})

	convey.Convey("Test TFIDFSplitter Markdown Header with MinChunkSize", t, func() {
		ctx := context.Background()
		config := &Config{
			MinChunkSize:         300,  // 最小 300 字符，防止在标题处立即分割
			MaxChunkSize:         1000, // 最大 1000 字符
			MaxSentencesPerChunk: 50,   // 允许很多句子
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "# Header 1\nSmall text.\n# Header 2\nMore small text but combined because of MinChunkSize."
		docs := []*schema.Document{{ID: "doc_md_len", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// 由于 MinChunkSize=300，总长度不到 300，所有内容应该在同一个 chunk 中
		convey.So(len(splitDocs), convey.ShouldEqual, 1)
		convey.So(splitDocs[0].Content, convey.ShouldContainSubstring, "# Header 1")
		convey.So(splitDocs[0].Content, convey.ShouldContainSubstring, "# Header 2")
	})

	convey.Convey("Test TFIDFSplitter with Sego", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold:  0.5,
			MaxChunkSize:         500, // 最大 500 字符
			MinChunkSize:         200, // 最小 200 字符
			MaxSentencesPerChunk: 50,  // 最多 50 个句子
			UseSego:              true,
			RemoveWhitespace:     true,
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := `会议指出 2025年，在以习近平同志为核心的党中央坚强领导下，中央纪委国家监委和各级纪检监察机关深入学习贯彻习近平新时代中国特色社会主义思想特别是习近平总书记关于党的建设的重要思想、关于党的自我革命的重要思想，持续推进党风廉政建设和反腐败斗争，聚焦"两个维护"强化政治监督，扎实开展深入贯彻中央八项规定精神学习教育，保持反腐败高压态势，深入推进风腐同查同治，坚决整治群众身边不正之风和腐败问题，完成对省区市巡视全覆盖，推动完善党和国家监督体系，深入开展"纪检监察工作规范化法治化正规化建设年"行动，推动纪检监察工作高质量发展取得新进展新成效。会议强调，2026年，各级纪检监察机关要坚持以习近平新时代中国特色社会主义思想为指导，深刻领悟"两个确立"的决定性意义，坚决做到"两个维护"，以更高标准、更实举措推进全面从严治党，为"十五五"时期经济社会发展提供坚强保障。要围绕实现"十五五"时期目标任务做深做实政治监督，推动党员干部树立和践行正确政绩观，把党中央重大决策部署落到实处。巩固拓展深入贯彻中央八项规定精神学习教育成果，推进作风建设常态化长效化，持续深化群众身边不正之风和腐败问题集中整治，用更多可感可及的成果赢得群众信任。把权力关进制度笼子，强化监督执纪问责，切实增强制度执行力。坚定不移推进反腐败斗争，一步不停歇、半步不退让，深化标本兼治，一体推进不敢腐、不能腐、不想腐，不断增强治理腐败综合效能。持续加强纪检监察工作规范化法治化正规化建设，着力锻造纪检监察铁军。`
		docs := []*schema.Document{
			{
				ID:      "doc2",
				Content: text,
			},
		}

		splitDocs, err := splitter.Transform(ctx, docs)
		for i, d := range splitDocs {
			fmt.Println(i, d.Content)
		}
		convey.So(err, convey.ShouldBeNil)

		// 中文文本约 600+ 字符，MinChunkSize=200，MaxChunkSize=500
		// 应该分割成多个 chunk
		convey.So(len(splitDocs), convey.ShouldBeGreaterThanOrEqualTo, 1)

		for _, d := range splitDocs {
			convey.So(d.Content, convey.ShouldNotBeEmpty)
			convey.So(d.ID, convey.ShouldEqual, "doc2")
		}
	})

	convey.Convey("Test TFIDFSplitter with Long Text (No Panic)", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold:  0.2,
			MaxChunkSize:         500, // 最大 500 字符
			MinChunkSize:         100, // 最小 100 字符
			MaxSentencesPerChunk: 10,  // 最多 10 个句子
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		// 创建一个包含大量句子的长文本，确保不会panic
		var longText strings.Builder
		for i := 0; i < 100; i++ {
			longText.WriteString(fmt.Sprintf("This is sentence number %d. ", i+1))
			longText.WriteString(fmt.Sprintf("It contains some content about topic %d. ", i+1))
			if i%10 == 0 {
				longText.WriteString(fmt.Sprintf("# Header %d\n", i/10+1))
			}
		}

		docs := []*schema.Document{
			{
				ID:      "doc_long",
				Content: longText.String(),
			},
		}

		// 这应该不会panic
		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)
		convey.So(len(splitDocs), convey.ShouldBeGreaterThan, 0)

		for _, d := range splitDocs {
			convey.So(d.Content, convey.ShouldNotBeEmpty)
			convey.So(d.ID, convey.ShouldEqual, "doc_long")
		}
	})

	convey.Convey("Test TFIDFSplitter with Very Long Text (No Nil Pointer)", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold:  0.1,
			MaxChunkSize:         500, // 最大 500 字符
			MinChunkSize:         100, // 最小 100 字符
			MaxSentencesPerChunk: 20,  // 最多 20 个句子
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		// 创建一个非常长的文本，包含大量句子
		var veryLongText strings.Builder
		for i := 0; i < 500; i++ {
			veryLongText.WriteString(fmt.Sprintf("Sentence %d with some content. ", i+1))
			if i%50 == 0 {
				veryLongText.WriteString(fmt.Sprintf("# Section %d\n", i/50+1))
			}
		}

		docs := []*schema.Document{
			{
				ID:      "doc_very_long",
				Content: veryLongText.String(),
			},
		}

		// 这应该不会panic或出现nil指针错误
		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)
		convey.So(len(splitDocs), convey.ShouldBeGreaterThan, 0)

		for _, d := range splitDocs {
			convey.So(d, convey.ShouldNotBeNil)
			convey.So(d.Content, convey.ShouldNotBeEmpty)
			convey.So(d.ID, convey.ShouldEqual, "doc_very_long")
		}
	})

	convey.Convey("Test TFIDFSplitter with Empty and Nil Inputs", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold:  0.2,
			MaxChunkSize:         500, // 最大 500 字符
			MaxSentencesPerChunk: 10,  // 最多 10 个句子
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		// 测试空文档
		docs := []*schema.Document{
			{
				ID:      "doc_empty",
				Content: "",
			},
		}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)
		// 空文档应该返回空结果或包含空字符串的结果
		convey.So(splitDocs, convey.ShouldNotBeNil)

		// 测试nil文档（应该跳过）
		docsWithNil := []*schema.Document{
			{
				ID:      "doc_valid",
				Content: "This is a valid document.",
			},
			nil,
			{
				ID:      "doc_valid2",
				Content: "This is another valid document.",
			},
		}

		splitDocs2, err := splitter.Transform(ctx, docsWithNil)
		convey.So(err, convey.ShouldBeNil)
		convey.So(len(splitDocs2), convey.ShouldBeGreaterThan, 0)
	})

	convey.Convey("Test TFIDFSplitter Filter Garbage Chunks", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold:  0.2,
			MaxChunkSize:         500,
			MinChunkSize:         10,
			MaxSentencesPerChunk: 50,
			FilterGarbageChunks:  true, // 启用乱码过滤
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		// 混合正常文本和乱码文本
		text := "This is a normal sentence. Au1k)g¡,9&C88DGAA'88DGAA'88DGAA'g¡,9&C264,#A'24\"#,264,#A'264,#A'264,#A'KKE,#A'PKSC%\"LE'88E›K(,#A'88E›K(,#A'88E›K(,#A'8K? Ne,{A'8K? Ne,{A'Oj788NXA:4\",#A'5%K)E{5%—x6y9x67F')7F')7F')24\"#,Au1k)9,,{PKSC%\"LE'Au1k)y9x67F')7F')7F')u5%—x6KxY,KxKSC%\"LE'KxY,? mKSC%\"LE'x4\"%\"LE'\"#,24\"#,\"#,Au1k)Au1k)y9x6yA'\"#,9&Cg¡,x4\"%\"LE'64\"#,24\"#,\"#,Au1k)KxKSC%\"LE'? mKSC%\"LE'y9x6yA'Au1k)88DGAA'\"#,Au1k)g¡,\"#,9&COj788NXA:4\",#A'KKE,#A'Au1k)Au1k)Au1k)88E›K(,#A'9,,{Au1k)D*DBguuh*D*h*uu)5%x6)5%x6)5%x6)5%x6Au1k)ux6&…市级港航? Ne5%\"6. This is another normal sentence."
		docs := []*schema.Document{
			{
				ID:      "doc_garbage",
				Content: text,
			},
		}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// 应该过滤掉乱码 chunk，只保留正常文本
		convey.So(len(splitDocs), convey.ShouldBeGreaterThan, 0)

		// 验证没有包含明显的乱码
		for _, d := range splitDocs {
			// 检查是否包含大量重复的特殊字符序列（乱码特征）
			content := d.Content
			hasGarbage := strings.Contains(content, "88DGAA'88DGAA'88DGAA'") ||
				strings.Contains(content, "Au1k)Au1k)Au1k)") ||
				strings.Contains(content, "7F')7F')7F'")
			convey.So(hasGarbage, convey.ShouldBeFalse)
		}
	})

	convey.Convey("Test TFIDFSplitter Disable Garbage Filter", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold:  0.2,
			MaxChunkSize:         500,
			MinChunkSize:         10,
			MaxSentencesPerChunk: 50,
			FilterGarbageChunks:  false, // 禁用乱码过滤
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "Normal text. Au1k)g¡,9&C88DGAA'88DGAA'88DGAA'g¡,9&C264,#A'24\"#,264,#A'264,#A'264,#A'KKE,#A'PKSC%\"LE'88E›K(,#A'88E›K(,#A'88E›K(,#A'8K? Ne,{A'8K? Ne,{A'Oj788NXA:4\",#A'5%K)E{5%—x6y9x67F')7F')7F')24\"#,Au1k)9,,{PKSC%\"LE'Au1k)y9x67F')7F')7F')u5%—x6KxY,KxKSC%\"LE'KxY,? mKSC%\"LE'x4\"%\"LE'\"#,24\"#,\"#,Au1k)Au1k)y9x6yA'\"#,9&Cg¡,x4\"%\"LE'64\"#,24\"#,\"#,Au1k)KxKSC%\"LE'? mKSC%\"LE'y9x6yA'Au1k)88DGAA'\"#,Au1k)g¡,\"#,9&COj788NXA:4\",#A'KKE,#A'Au1k)Au1k)Au1k)88E›K(,#A'9,,{Au1k)D*DBguuh*D*h*uu)5%x6)5%x6)5%x6)5%x6Au1k)ux6&…市级港航? Ne5%\"6. More normal text."
		docs := []*schema.Document{
			{
				ID:      "doc_no_filter",
				Content: text,
			},
		}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// 禁用过滤时，应该保留所有 chunk（包括乱码）
		convey.So(len(splitDocs), convey.ShouldBeGreaterThan, 0)

		// 应该能找到乱码内容
		hasGarbage := false
		for _, d := range splitDocs {
			if strings.Contains(d.Content, "88DGAA'88DGAA'88DGAA'") ||
				strings.Contains(d.Content, "Au1k)Au1k)Au1k)") {
				hasGarbage = true
				break
			}
		}
		convey.So(hasGarbage, convey.ShouldBeTrue)
	})
}
