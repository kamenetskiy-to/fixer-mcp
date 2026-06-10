//go:build ignore

package main

import (
	"log"
)

func main() {
	log.Fatal("Deprecated migration path. Use `go run ./cmd/project_doc_hard_replace --source-dsn <dsn>`; legacy filesystem project_book ingest is disabled.")
}
