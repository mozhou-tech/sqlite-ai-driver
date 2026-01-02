module github.com/mozhou-tech/sqlite-ai-driver/pkg/graphstore

go 1.24.2

require (
	github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver v0.0.0
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite-driver v0.0.0
)

require (
	github.com/adamzy/cedar-go v0.0.0-20170805034717-80a9c64b256d // indirect
	github.com/apache/arrow-go/v18 v18.1.0 // indirect
	github.com/duckdb/duckdb-go-bindings v0.1.9 // indirect
	github.com/duckdb/duckdb-go-bindings/darwin-amd64 v0.1.4 // indirect
	github.com/duckdb/duckdb-go-bindings/darwin-arm64 v0.1.4 // indirect
	github.com/duckdb/duckdb-go-bindings/linux-amd64 v0.1.4 // indirect
	github.com/duckdb/duckdb-go-bindings/linux-arm64 v0.1.4 // indirect
	github.com/duckdb/duckdb-go-bindings/windows-amd64 v0.1.4 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/google/flatbuffers v25.1.24+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/huichen/sego v0.0.0-20210824061530-c87651ea5c76 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/marcboeker/go-duckdb/arrowmapping v0.0.2 // indirect
	github.com/marcboeker/go-duckdb/mapping v0.0.2 // indirect
	github.com/marcboeker/go-duckdb/v2 v2.0.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sego v0.0.0 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	golang.org/x/exp v0.0.0-20250620022241-b7579e27df2b // indirect
	golang.org/x/mod v0.25.0 // indirect
	golang.org/x/sync v0.15.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/tools v0.34.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	modernc.org/libc v1.66.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.38.2 // indirect
)

replace (
	github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver => ../cayley-driver
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite-driver => ../sqlite-driver
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sego => ../sego
)
