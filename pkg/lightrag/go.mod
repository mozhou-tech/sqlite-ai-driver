module github.com/mozhou-tech/sqlite-ai-driver/pkg/lightrag

go 1.24.2

require (
	github.com/cloudwego/eino v0.7.14
	github.com/cloudwego/eino-ext/components/embedding/openai v0.0.0-20251226123311-1d93d527c144
	github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver v0.0.0
	github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver v0.0.0
	github.com/sirupsen/logrus v1.9.3
	golang.org/x/sync v0.18.0
	golang.org/x/time v0.5.0
)

replace (
	github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver => ../cayley-driver
	github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver => ../duckdb-driver
)

