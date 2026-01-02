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
	"github.com/cloudwego/eino/components/retriever"
)

type implOptions struct {
	// MetadataFilter filters documents by metadata fields.
	// Each key-value pair in the map represents a filter condition.
	// For example: map[string]any{"category": "tech", "author": "CloudWeGo"}
	// will only return documents where metadata.category = "tech" AND metadata.author = "CloudWeGo"
	MetadataFilter map[string]any
}

// WithMetadataFilter sets metadata filter for vector search.
// The filter is a map of key-value pairs that will be used to filter documents
// based on their metadata JSON fields in SQLite.
func WithMetadataFilter(filter map[string]any) retriever.Option {
	return retriever.WrapImplSpecificOptFn(func(o *implOptions) {
		o.MetadataFilter = filter
	})
}
