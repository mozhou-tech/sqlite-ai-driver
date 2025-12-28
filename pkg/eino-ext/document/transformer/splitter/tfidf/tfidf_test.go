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
			SimilarityThreshold: 0.1,
			MaxChunkSize:        2,
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

		// Since we set MaxChunkSize to 2, we expect at least 3 chunks (6 sentences total)
		convey.So(len(splitDocs), convey.ShouldBeGreaterThanOrEqualTo, 3)

		for _, d := range splitDocs {
			convey.So(d.Content, convey.ShouldNotBeEmpty)
			convey.So(d.ID, convey.ShouldEqual, "doc1") // Default ID generator keeps original ID
		}
	})

	convey.Convey("Test TFIDFSplitter Markdown Header and Whitespace", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold: 0.1,
			MaxChunkSize:        10,
			MinChunkSize:        5,
			RemoveWhitespace:    false,
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
			MinChunkSize: 50, // Force combining sentences
			MaxChunkSize: 10,
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "Short. Very short. Still short. Now it should be long enough to split if the next one is added but we check length."
		docs := []*schema.Document{{ID: "doc_len", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		for _, d := range splitDocs {
			convey.So(len(d.Content), convey.ShouldBeGreaterThanOrEqualTo, 50)
		}
	})

	convey.Convey("Test TFIDFSplitter MaxChunkSize Soft Limit", t, func() {
		ctx := context.Background()
		config := &Config{
			MaxChunkSize: 2,
			MinChunkSize: 1000, // Very large, MaxChunkSize should be a soft limit
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "Sentence one. Sentence two. Sentence three. Sentence four."
		docs := []*schema.Document{{ID: "doc_max", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// Should NOT split because MinChunkSize is not met and MaxChunkSize*2 is not reached
		convey.So(len(splitDocs), convey.ShouldEqual, 1)
		convey.So(splitDocs[0].Content, convey.ShouldContainSubstring, "Sentence four.")
	})

	convey.Convey("Test TFIDFSplitter MaxChunkSize Hard Limit (ForceSplit)", t, func() {
		ctx := context.Background()
		config := &Config{
			MaxChunkSize: 1,
			MinChunkSize: 1000, // Very large
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		// 3 sentences, MaxChunkSize=1, so forceSplit at 2 sentences
		text := "Sentence one. Sentence two. Sentence three."
		docs := []*schema.Document{{ID: "doc_force", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// i=1 (Sent 2): len(currentChunk)=1. forceSplit = 1 >= 2 (false). shouldSplit=true, canSplit=false.合并.
		// i=2 (Sent 3): len(currentChunk)=2. forceSplit = 2 >= 2 (true). 切分.
		// 结果应该是 2 个 chunk: [Sent 1 + Sent 2], [Sent 3]
		convey.So(len(splitDocs), convey.ShouldEqual, 2)
	})

	convey.Convey("Test TFIDFSplitter Markdown Header with MinChunkSize", t, func() {
		ctx := context.Background()
		config := &Config{
			MinChunkSize: 300, // Large enough to prevent immediate split at header
			MaxChunkSize: 1000,
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "# Header 1\nSmall text.\n# Header 2\nMore small text but combined because of MinChunkSize."
		docs := []*schema.Document{{ID: "doc_md_len", Content: text}}

		splitDocs, err := splitter.Transform(ctx, docs)
		convey.So(err, convey.ShouldBeNil)

		// Both headers should be in the same chunk because total length is less than 100
		convey.So(len(splitDocs), convey.ShouldEqual, 1)
		convey.So(splitDocs[0].Content, convey.ShouldContainSubstring, "# Header 1")
		convey.So(splitDocs[0].Content, convey.ShouldContainSubstring, "# Header 2")
	})

	convey.Convey("Test TFIDFSplitter with Sego", t, func() {
		ctx := context.Background()
		config := &Config{
			SimilarityThreshold: 0.5,
			MaxChunkSize:        500,
			UseSego:             true,
			MinChunkSize:        500,
			RemoveWhitespace:    true,
		}

		splitter, err := NewTFIDFSplitter(ctx, config)
		convey.So(err, convey.ShouldBeNil)

		text := "会议指出 2025年，在以习近平同志为核心的党中央坚强领导下，中央纪委国家监委和各级纪检监察机关深入学习贯彻习近平新时代中国特色社会主义思想特别是习近平总书记关于党的建设的重要思想、关于党的自我革命的重要思想，持续推进党风廉政建设和反腐败斗争，聚焦“两个维护”强化政治监督，扎实开展深入贯彻中央八项规定精神学习教育，保持反腐败高压态势，深入推进风腐同查同治，坚决整治群众身边不正之风和腐败问题，完成对省区市巡视全覆盖，推动完善党和国家监督体系，深入开展“纪检监察工作规范化法治化正规化建设年”行动，推动纪检监察工作高质量发展取得新进展新成效。会议强调，2026年，各级纪检监察机关要坚持以习近平新时代中国特色社会主义思想为指导，深刻领悟“两个确立”的决定性意义，坚决做到“两个维护”，以更高标准、更实举措推进全面从严治党，为“十五五”时期经济社会发展提供坚强保障。要围绕实现“十五五”时期目标任务做深做实政治监督，推动党员干部树立和践行正确政绩观，把党中央重大决策部署落到实处。巩固拓展深入贯彻中央八项规定精神学习教育成果，推进作风建设常态化长效化，持续深化群众身边不正之风和腐败问题集中整治，用更多可感可及的成果赢得群众信任。把权力关进制度笼子，强化监督执纪问责，切实增强制度执行力。坚定不移推进反腐败斗争，一步不停歇、半步不退让，深化标本兼治，一体推进不敢腐、不能腐、不想腐，不断增强治理腐败综合效能。持续加强纪检监察工作规范化法治化正规化建设，着力锻造纪检监察铁军。"
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

		// With sego, it should be able to tokenize Chinese correctly.
		// Since we set MaxChunkSize to 2 and there are 6 sentences, we expect ~3 chunks.
		convey.So(len(splitDocs), convey.ShouldBeGreaterThanOrEqualTo, 3)

		for _, d := range splitDocs {
			convey.So(d.Content, convey.ShouldNotBeEmpty)
			convey.So(d.ID, convey.ShouldEqual, "doc2")
		}
	})
}
