module github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver

go 1.24.2

require (
	github.com/marcboeker/go-duckdb/v2 v2.0.0
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sego v0.0.0
)

replace (
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sego => ../sego
)

