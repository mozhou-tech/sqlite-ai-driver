# go-sqlite3-fts5

Importing this package will enable the FTS5 extension with github.com/mattn/go-sqlite3 package.

## Usage

```go
import _ "github.com/knaka/go-sqlite3-fts5"
```

## Update the source code

`go generate` will fetch the FTS5 source code of the same version as the go-sqlite3 package which is specified in the go.mod file. 
