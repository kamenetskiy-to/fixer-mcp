//go:build ignore

package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "fixer.db")
	if err != nil {
		log.Fatalf("Error opening db: %v", err)
	}
	defer db.Close()

	// Parse db_dump.sql for codex_thread strings
	file, err := os.Open("db_dump.sql")
	if err != nil {
		log.Fatalf("Error opening db_dump.sql: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// we might need a large buffer for massive lines in dump
	const maxCapacity = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	inThreadBlock := false
	projectsToMigrate := make(map[string]string)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "COPY public.codex_thread ") {
			inThreadBlock = true
			continue
		}

		if inThreadBlock {
			if strings.HasPrefix(line, "\\.") {
				inThreadBlock = false
				continue
			}

			parts := strings.Split(line, "\t")
			if len(parts) >= 4 {
				title := parts[2]
				cwd := parts[3]

				if cwd != "\\N" && cwd != "" {
					// map cwd to the title. Latest thread per cwd wins or overwrite
					cwd = strings.TrimSuffix(cwd, "/fixer_mcp")
					projectsToMigrate[cwd] = title
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Scanner error: %v", err)
	}

	for cwd, title := range projectsToMigrate {
		// Clean up title
		name := title
		if name == "" || name == "\\N" {
			name = filepath.Base(cwd)
		} else {
			// clean up prefixes
			name = strings.TrimPrefix(name, "Orchestrator: ")
			name = strings.TrimPrefix(name, "Session: ")
			// remove the "(archived ...)" suffix
			if idx := strings.Index(name, " (archived "); idx != -1 {
				name = name[:idx]
			}
		}

		// Check if it exists
		var id int
		err := db.QueryRow("SELECT id FROM project WHERE cwd = ?", cwd).Scan(&id)
		if err == sql.ErrNoRows {
			res, err := db.Exec("INSERT INTO project (name, cwd) VALUES (?, ?)", name, cwd)
			if err != nil {
				log.Printf("Failed to insert project %s: %v", name, err)
				continue
			}
			insId, _ := res.LastInsertId()
			id = int(insId)
			log.Printf("Inserted project ID %d: %s (%s)", id, name, cwd)
		} else if err != nil {
			log.Printf("Error checking project %s: %v", name, err)
			continue
		} else {
			log.Printf("Project already exists: %s (%s)", name, cwd)
		}

		// Also migrate documentation
		// In absence of actual project_doc in dump, we fall back to creating a dummy migrated doc
		// OR we can read the README.md if it exists
		var docCount int
		db.QueryRow("SELECT COUNT(*) FROM project_doc WHERE project_id = ?", id).Scan(&docCount)
		if docCount == 0 {
			dummyContent := fmt.Sprintf("Migrated relative configuration and notes for %s (legacy)", name)

			// Try reading actual README
			readmePath := filepath.Join(cwd, "README.md")
			readmeData, err := os.ReadFile(readmePath)
			if err == nil {
				dummyContent = string(readmeData)
			}

			_, docErr := db.Exec("INSERT INTO project_doc (project_id, title, content) VALUES (?, ?, ?)", id, "Legacy Migrated Doc", dummyContent)
			if docErr != nil {
				log.Printf("Failed to insert doc for project %d: %v", id, docErr)
			} else {
				log.Printf("Inserted legacy doc for project %d", id)
			}
		}
	}
	log.Println("Migration complete!")
}
