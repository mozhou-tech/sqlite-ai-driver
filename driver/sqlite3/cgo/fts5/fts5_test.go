package fts5

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"testing"
)

func TestFts5(t *testing.T) {
	filePath := "test.db"
	_ = os.Remove(filePath)
	db, err := sql.Open("sqlite3", filePath)
	if err != nil {
		t.Fatal(err)
	}
	defer (func() { _ = db.Close() })()
	t.Cleanup(func() { _ = os.Remove(filePath) })
	_, err = db.Exec("CREATE VIRTUAL TABLE fts USING fts5(body, tokenize=unicode61)")
	if err != nil {
		t.Fatal(err)
	}
}
