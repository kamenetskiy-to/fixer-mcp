package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"fixer_mcp/dashboard_api"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "listen address")
	dbPath := flag.String("db", "", "path to fixer.db")
	projectCWD := flag.String("cwd", "", "current project cwd for binding derivation")
	flag.Parse()

	currentCWD := *projectCWD
	if currentCWD == "" {
		if wd, err := os.Getwd(); err == nil {
			currentCWD = wd
		}
	}

	repo, err := dashboardapi.OpenRepository(*dbPath, currentCWD)
	if err != nil {
		log.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	server := &http.Server{
		Addr:    *addr,
		Handler: dashboardapi.NewServer(repo),
	}

	log.Printf("dashboard_api listening on http://%s", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("dashboard_api server failed: %v", err)
	}
}
