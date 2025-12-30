module github.com/mozhou-tech/sqlite-ai-driver/pkg/eino-ext

go 1.24.2

require (
	github.com/cloudwego/eino v0.7.14
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
	github.com/rioloc/tfidf-go v0.0.0-20250724175239-3a8f9fe7e629
	github.com/sirupsen/logrus v1.9.3
)

require (
	github.com/mozhou-tech/sqlite-ai-driver v0.0.0
	github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver v0.0.0
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sego v0.0.0
)

replace (
	github.com/mozhou-tech/sqlite-ai-driver => ../..
	github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver => ../cayley-driver
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sego => ../sego
)

