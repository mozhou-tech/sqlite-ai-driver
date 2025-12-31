/*
 * Copyright 2025 CloudWeGo Authors
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

package duckdb

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/vecstore"
	"github.com/smartystreets/goconvey/convey"
)

// getProjectRootTestdata 获取工程根目录的 testdata 路径
func getProjectRootTestdata() string {
	wd, _ := os.Getwd()
	if filepath.Base(wd) == "dockdb" {
		return filepath.Join(wd, "..", "..", "..", "testdata")
	}
	// 尝试查找 go.mod 文件来确定工程根目录
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "testdata")
		}
	}
	return filepath.Join("..", "..", "..", "testdata")
}

func TestNewRetriever(t *testing.T) {
	PatchConvey("test NewRetriever", t, func() {
		ctx := context.Background()

		// Create VecStore instance for testing
		vecStore := vecstore.New(vecstore.Options{
			Embedder: nil, // VecStore doesn't require embedder for initialization
		})
		if err := vecStore.Initialize(ctx); err != nil {
			t.Fatalf("Failed to initialize vecstore: %v", err)
		}
		defer vecStore.Close()

		PatchConvey("test embedding not provided", func() {
			r, err := NewRetriever(ctx, &RetrieverConfig{
				VecStore:  vecStore,
				Embedding: nil,
			})
			convey.So(err, convey.ShouldBeError, fmt.Errorf("[NewRetriever] embedding not provided for duckdb retriever"))
			convey.So(r, convey.ShouldBeNil)
		})

		PatchConvey("test vecstore not provided", func() {
			r, err := NewRetriever(ctx, &RetrieverConfig{
				VecStore:  nil,
				Embedding: &mockEmbedding{},
			})
			convey.So(err, convey.ShouldBeError, fmt.Errorf("[NewRetriever] vecstore instance not provided, must pass a *vecstore.VecStore instance"))
			convey.So(r, convey.ShouldBeNil)
		})

		PatchConvey("test success", func() {
			r, err := NewRetriever(ctx, &RetrieverConfig{
				VecStore:  vecStore,
				Embedding: &mockEmbedding{},
			})
			convey.So(err, convey.ShouldBeNil)
			convey.So(r, convey.ShouldNotBeNil)
		})
	})
}

func TestRetrieve(t *testing.T) {
	PatchConvey("test Retrieve", t, func() {
		ctx := context.Background()
		expv := make([]float64, 10)
		for i := range expv {
			expv[i] = 1.1
		}
		// d1 := &schema.Document{ID: "1", Content: "asd"}
		// d1.WithDenseVector(expv)
		// d2 := &schema.Document{ID: "2", Content: "qwe"}
		// d2.WithDenseVector(expv)
		// docs := []*schema.Document{d1, d2}

		PatchConvey("test Embedding not provided", func() {
			r := &Retriever{config: &RetrieverConfig{Embedding: nil}}
			resp, err := r.Retrieve(ctx, "test_query")
			convey.So(err, convey.ShouldBeError, fmt.Errorf("[duckdb retriever] embedding not provided"))
			convey.So(resp, convey.ShouldBeNil)
		})

		PatchConvey("test Embedding error", func() {
			mockErr := fmt.Errorf("mock err")
			r := &Retriever{config: &RetrieverConfig{Embedding: &mockEmbedding{err: mockErr}}}
			resp, err := r.Retrieve(ctx, "test_query")
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, "mock err")
			convey.So(resp, convey.ShouldBeNil)
		})

		PatchConvey("test vector size invalid", func() {
			r := &Retriever{config: &RetrieverConfig{Embedding: &mockEmbedding{sizeForCall: []int{2}, dims: 10}}}
			resp, err := r.Retrieve(ctx, "test_query")
			convey.So(err, convey.ShouldBeError, fmt.Errorf("[duckdb retriever] invalid return length of vector, got=2, expected=1"))
			convey.So(resp, convey.ShouldBeNil)
		})

		// Redis-specific tests are removed as they are no longer relevant.
	})
}

type mockEmbedding struct {
	err         error
	cnt         int
	sizeForCall []int
	dims        int
}

func (m *mockEmbedding) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	if m.cnt > len(m.sizeForCall) {
		log.Fatal("unexpected")
	}

	if m.err != nil {
		return nil, m.err
	}

	slice := make([]float64, m.dims)
	for i := range slice {
		slice[i] = 1.1
	}

	r := make([][]float64, m.sizeForCall[m.cnt])
	m.cnt++
	for i := range r {
		r[i] = slice
	}

	return r, nil
}
