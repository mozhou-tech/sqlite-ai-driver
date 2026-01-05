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

require (
	github.com/adamzy/cedar-go v0.0.0-20170805034717-80a9c64b256d // indirect
	github.com/apache/arrow-go/v18 v18.1.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.14.1 // indirect
	github.com/bytedance/sonic/loader v0.3.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/cloudwego/eino-ext/libs/acl/openai v0.1.2 // indirect
	github.com/duckdb/duckdb-go-bindings v0.1.9 // indirect
	github.com/duckdb/duckdb-go-bindings/darwin-amd64 v0.1.4 // indirect
	github.com/duckdb/duckdb-go-bindings/darwin-arm64 v0.1.4 // indirect
	github.com/duckdb/duckdb-go-bindings/linux-amd64 v0.1.4 // indirect
	github.com/duckdb/duckdb-go-bindings/linux-arm64 v0.1.4 // indirect
	github.com/duckdb/duckdb-go-bindings/windows-amd64 v0.1.4 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/eino-contrib/jsonschema v1.0.3 // indirect
	github.com/evanphx/json-patch v0.5.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/google/flatbuffers v25.1.24+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/goph/emperror v0.17.2 // indirect
	github.com/huichen/sego v0.0.0-20210824061530-c87651ea5c76 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/marcboeker/go-duckdb/arrowmapping v0.0.2 // indirect
	github.com/marcboeker/go-duckdb/mapping v0.0.2 // indirect
	github.com/marcboeker/go-duckdb/v2 v2.0.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/meguminnnnnnnnn/go-openai v0.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sego v0.0.0 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/nikolalohinski/gonja v1.5.3 // indirect
	github.com/pelletier/go-toml/v2 v2.2.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/slongfield/pyfmt v0.0.0-20220222012616-ea85ff4c361f // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/yargevad/filepathx v1.0.0 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	golang.org/x/arch v0.15.0 // indirect
	golang.org/x/exp v0.0.0-20250620022241-b7579e27df2b // indirect
	golang.org/x/mod v0.25.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/tools v0.34.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.66.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.38.2 // indirect
)

replace (
	github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver => ../cayley-driver
	github.com/mozhou-tech/sqlite-ai-driver/pkg/duckdb-driver => ../duckdb-driver
	github.com/mozhou-tech/sqlite-ai-driver/pkg/sego => ../sego
)
