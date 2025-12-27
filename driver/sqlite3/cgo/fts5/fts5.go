package fts5

//go:generate go run ./update-sqlite3-sources/ sqlite3.h sqlite3ext.h fts5.h fts5.c

/*
#cgo LDFLAGS: -lm

#include "fts5.h"

int sqlite3_fts_init(sqlite3 *, char * *, const sqlite3_api_routines *);

void init_go_sqlite3_fts5 (void) {
    sqlite3_auto_extension((void *) sqlite3_fts_init);
}
*/
import "C"

import (
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	C.init_go_sqlite3_fts5()
}
