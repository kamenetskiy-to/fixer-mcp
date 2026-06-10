package dashboardapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	repo *Repository
	mux  *http.ServeMux
}

func NewServer(repo *Repository) *Server {
	s := &Server{
		repo: repo,
		mux:  http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/home", s.handleHome)
	s.mux.HandleFunc("/api/projects/", s.handleProjects)
	s.mux.HandleFunc("/api/sessions/", s.handleSessionDetail)
	s.mux.HandleFunc("/api/actions/projects/", s.handleProjectActions)
	s.mux.HandleFunc("/api/actions/sessions/", s.handleSessionActions)
	s.mux.HandleFunc("/api/actions/proposals/", s.handleProposalActions)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	payload, err := s.repo.Health(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	payload, err := s.repo.HomeSnapshot(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeErrorMessage(w, http.StatusNotFound, "project route not found")
		return
	}
	parts := strings.Split(path, "/")
	projectID, err := strconv.Atoi(parts[0])
	if err != nil || projectID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid project id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if len(parts) == 1 || parts[1] == "snapshot" {
		payload, err := s.repo.ProjectSnapshot(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}

	switch parts[1] {
	case "overview":
		payload, err := s.repo.ProjectOverview(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "docs":
		payload, err := s.repo.ProjectDocs(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "netrunners":
		payload, err := s.repo.ProjectNetrunners(ctx, projectID, strings.Split(r.URL.Query().Get("status"), ","))
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "fixer-chat-binding":
		payload, err := s.repo.FixerChatBinding(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "overseer-chat-binding":
		payload, err := s.repo.OverseerChatBinding(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	default:
		writeErrorMessage(w, http.StatusNotFound, "project route not found")
	}
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	sessionID, err := strconv.Atoi(path)
	if err != nil || sessionID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid session id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	payload, err := s.repo.NetrunnerDetail(ctx, sessionID)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleProjectActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/actions/projects/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "tasks" {
		writeErrorMessage(w, http.StatusNotFound, "project action route not found")
		return
	}
	projectID, err := strconv.Atoi(parts[0])
	if err != nil || projectID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var input CreateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	payload, err := s.repo.CreateTask(ctx, projectID, input)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleSessionActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/actions/sessions/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		writeErrorMessage(w, http.StatusNotFound, "session action route not found")
		return
	}
	sessionID, err := strconv.Atoi(parts[0])
	if err != nil || sessionID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid session id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch parts[1] {
	case "attached-docs":
		var input SetSessionAttachedDocsInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.SetSessionAttachedDocs(ctx, sessionID, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "mcp-servers":
		var input SetSessionMCPServersInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.SetSessionMCPServers(ctx, sessionID, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "status":
		var input SetSessionStatusInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.SetSessionStatus(ctx, sessionID, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	default:
		writeErrorMessage(w, http.StatusNotFound, "session action route not found")
	}
}

func (s *Server) handleProposalActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/actions/proposals/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "status" {
		writeErrorMessage(w, http.StatusNotFound, "proposal action route not found")
		return
	}
	proposalID, err := strconv.Atoi(parts[0])
	if err != nil || proposalID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid proposal id")
		return
	}
	var input SetProposalStatusInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	payload, err := s.repo.SetProposalStatus(ctx, proposalID, input)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeRepoError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorMessage(w, http.StatusNotFound, "record not found")
		return
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "invalid "),
		strings.Contains(message, "unknown "),
		strings.Contains(message, "not allowed"),
		strings.Contains(message, "required"),
		strings.Contains(message, "ambiguous"):
		writeErrorMessage(w, http.StatusBadRequest, message)
		return
	case strings.Contains(message, "frozen"):
		writeErrorMessage(w, http.StatusConflict, message)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeErrorMessage(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeErrorMessage(w, status, err.Error())
}

func writeErrorMessage(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
