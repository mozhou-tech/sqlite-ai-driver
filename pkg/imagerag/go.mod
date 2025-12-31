module github.com/mozhou-tech/sqlite-ai-driver/pkg/imagerag

go 1.24.2

require (
	github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver v0.0.0
	github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag v0.0.0
	github.com/sirupsen/logrus v1.9.3
	golang.org/x/sync v0.18.0
)

replace (
	github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver => ../duckdb-driver
	github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag => ../lightrag
)

