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

package rxdb

import (
	"context"
	"fmt"
	"log"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/mozhou-tech/rxdb-go/pkg/rxdb"
	"github.com/smartystreets/goconvey/convey"
)

func TestBulkStore(t *testing.T) {
	PatchConvey("test bulkStore", t, func() {
		ctx := context.Background()
		mockColl := &mockCollection{}
		d1 := &schema.Document{ID: "1", Content: "asd"}
		d2 := &schema.Document{ID: "2", Content: "qwe", MetaData: map[string]any{
			"mock_field_1": map[string]any{"extra_field_1": "asd"},
			"mock_field_2": int64(123),
		}}
		docs := []*schema.Document{d1, d2}

		PatchConvey("test DocumentToMap failed", func() {
			i := &Indexer{
				config: &IndexerConfig{
					Collection: mockColl,
					DocumentToMap: func(ctx context.Context, doc *schema.Document) (map[string]any, map[string]string, error) {
						return nil, nil, fmt.Errorf("mock err")
					},
					BatchSize: 10,
					Embedding: nil,
				},
			}

			convey.So(i.bulkStore(ctx, docs, &indexer.Options{
				Embedding: nil,
			}), convey.ShouldBeError, fmt.Errorf("mock err"))
		})

		PatchConvey("test embSize > i.config.BatchSize", func() {
			i := &Indexer{
				config: &IndexerConfig{
					Collection: mockColl,
					DocumentToMap: func(ctx context.Context, doc *schema.Document) (map[string]any, map[string]string, error) {
						return map[string]any{"content": doc.Content}, map[string]string{
							"content": "vector_content",
							"another": "another_vector",
						}, nil
					},
					BatchSize: 1,
					Embedding: nil,
				},
			}

			convey.So(i.bulkStore(ctx, docs, &indexer.Options{
				Embedding: nil,
			}), convey.ShouldBeError, fmt.Errorf("[bulkStore] embedding size over batch size, batch size=%d, got size=%d",
				i.config.BatchSize, 2))
		})

		PatchConvey("test embedding not provided error", func() {
			i := &Indexer{
				config: &IndexerConfig{
					Collection:    mockColl,
					DocumentToMap: defaultDocumentToMap,
					BatchSize:     1,
					Embedding:     nil,
				},
			}

			convey.So(i.bulkStore(ctx, docs, &indexer.Options{
				Embedding: nil,
			}), convey.ShouldBeError, fmt.Errorf("[bulkStore] embedding method not provided"))
		})

		PatchConvey("test embedding failed", func() {
			exp := fmt.Errorf("mock err")
			i := &Indexer{
				config: &IndexerConfig{
					Collection:    mockColl,
					DocumentToMap: defaultDocumentToMap,
					BatchSize:     1,
				},
			}

			convey.So(i.bulkStore(ctx, docs, &indexer.Options{
				Embedding: &mockEmbedding{err: exp},
			}), convey.ShouldBeError, fmt.Errorf("[bulkStore] embedding failed, %w", exp))
		})

		PatchConvey("test len(vectors) != len(texts)", func() {
			i := &Indexer{
				config: &IndexerConfig{
					Collection:    mockColl,
					DocumentToMap: defaultDocumentToMap,
					BatchSize:     1,
				},
			}

			convey.So(i.bulkStore(ctx, docs, &indexer.Options{
				Embedding: &mockEmbedding{sizeForCall: []int{2}, dims: 1024},
			}), convey.ShouldBeError, fmt.Errorf("[bulkStore] invalid vector length, expected=1, got=2"))
		})

		PatchConvey("test success", func() {
			var storedDocs []map[string]any
			mockColl.bulkUpsertFn = func(ctx context.Context, docs []map[string]any) ([]rxdb.Document, error) {
				storedDocs = append(storedDocs, docs...)
				return nil, nil
			}

			i := &Indexer{
				config: &IndexerConfig{
					Collection:    mockColl,
					DocumentToMap: defaultDocumentToMap,
					BatchSize:     1,
				},
			}

			convey.So(i.bulkStore(ctx, docs, &indexer.Options{
				Embedding: &mockEmbedding{sizeForCall: []int{1, 1}, dims: 1024},
			}), convey.ShouldBeNil)

			convey.So(len(storedDocs), convey.ShouldEqual, 2)

			slice := make([]float64, 1024)
			for i := range slice {
				slice[i] = 1.1
			}

			contains := func(doc *schema.Document, stored map[string]any) {
				convey.So(stored["id"], convey.ShouldEqual, doc.ID)
				convey.So(stored[defaultReturnFieldContent], convey.ShouldEqual, doc.Content)
				convey.So(stored[defaultReturnFieldVectorContent], convey.ShouldResemble, slice)
				for field, val := range doc.MetaData {
					convey.So(stored[field], convey.ShouldResemble, val)
				}
			}

			contains(d1, storedDocs[0])
			contains(d2, storedDocs[1])
		})
	})
}

type mockCollection struct {
	rxdb.Collection
	bulkUpsertFn func(ctx context.Context, docs []map[string]any) ([]rxdb.Document, error)
}

func (m *mockCollection) BulkUpsert(ctx context.Context, docs []map[string]any) ([]rxdb.Document, error) {
	if m.bulkUpsertFn != nil {
		return m.bulkUpsertFn(ctx, docs)
	}
	return nil, nil
}

type mockEmbedding struct {
	err         error
	cnt         int
	sizeForCall []int
	dims        int
}

func (m *mockEmbedding) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	if m.cnt >= len(m.sizeForCall) {
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
