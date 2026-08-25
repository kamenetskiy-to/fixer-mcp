package dashboardapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
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
	s.mux.HandleFunc("/internal/v1/projects/", s.handleInternalWorkroom)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/home", s.handleHome)
	s.mux.HandleFunc("/api/architect/orders", s.handleArchitectOrders)
	s.mux.HandleFunc("/api/overseer/threads", s.handleOverseerThreads)
	s.mux.HandleFunc("/api/projects/", s.handleProjects)
	s.mux.HandleFunc("/api/sessions/", s.handleSessionDetail)
	s.mux.HandleFunc("/api/actions/projects/", s.handleProjectActions)
	s.mux.HandleFunc("/api/actions/orders/", s.handleOrderActions)
	s.mux.HandleFunc("/api/orders/", s.handleOrderActions)
	s.mux.HandleFunc("/api/previews/", s.handlePreview)
	s.mux.HandleFunc("/api/actions/sessions/", s.handleSessionActions)
	s.mux.HandleFunc("/api/actions/proposals/", s.handleProposalActions)
	s.mux.HandleFunc("/api/actions/overseer/launch", s.handleOverseerLaunch)
}

func (s *Server) handleInternalWorkroom(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/projects/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		writeErrorMessage(w, http.StatusNotFound, "internal workroom route not found")
		return
	}
	projectID, err := strconv.Atoi(parts[0])
	if err != nil || projectID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid project id")
		return
	}
	body, err := readBoundedBridgeBody(r)
	if err != nil {
		writeErrorMessage(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	principal, err := s.authorizeWorkroomBridge(r, projectID, body)
	if err != nil {
		writeErrorMessage(w, http.StatusUnauthorized, "invalid signed workroom bridge request")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	switch {
	case r.Method == http.MethodGet && len(parts) == 3 && parts[1] == "workroom" && parts[2] == "snapshot":
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		payload, err := s.repo.ProjectWorkroomSnapshot(ctx, projectID, principal)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "events":
		afterSeq, err := strconv.ParseInt(r.URL.Query().Get("after_seq"), 10, 64)
		if err != nil || afterSeq < 0 {
			writeErrorMessage(w, http.StatusBadRequest, "invalid after_seq")
			return
		}
		limit := workroomMaxEventBatch
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > workroomMaxEventBatch {
				writeErrorMessage(w, http.StatusBadRequest, "invalid event limit")
				return
			}
		}
		waitMS := 25000
		if raw := strings.TrimSpace(r.URL.Query().Get("wait_ms")); raw != "" {
			waitMS, err = strconv.Atoi(raw)
			if err != nil || waitMS < 0 || waitMS > 25000 {
				writeErrorMessage(w, http.StatusBadRequest, "invalid wait_ms")
				return
			}
		}
		payload, err := s.repo.WaitProjectUIEvents(r.Context(), projectID, afterSeq, limit, time.Duration(waitMS)*time.Millisecond)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "fixer" && parts[2] == "turns":
		var input SendFixerTurnInput
		if err := decodeStrictBridgeJSON(body, &input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.SendFixerTurn(r.Context(), projectID, principal, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, payload)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "genui" && parts[2] == "surfaces":
		var input RequestGenuiSurfaceInput
		if err := decodeStrictBridgeJSON(body, &input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.RequestGenuiSurface(r.Context(), projectID, principal, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "genui" && parts[2] == "actions":
		var input GenuiActionRequest
		if err := decodeStrictBridgeJSON(body, &input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.InvokeGenuiAction(r.Context(), projectID, principal, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "hands":
		writeErrorMessage(w, http.StatusNotFound, "internal workroom route not found")
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "hands" && parts[2] == "instructions":
		var input BridgeSubmitHandsInstructionInput
		if err := decodeStrictBridgeJSON(body, &input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.SubmitHandsInstruction(r.Context(), projectID, principal, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, payload)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "hands" && parts[2] == "lane":
		var input BridgeSelectHandsLaneInput
		if err := decodeStrictBridgeJSON(body, &input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.SelectHandsLane(r.Context(), projectID, principal, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case r.Method == http.MethodPost && len(parts) == 5 && parts[1] == "hands" && parts[2] == "instructions" && parts[4] == "cancel":
		var input BridgeCancelHandsInstructionInput
		if err := decodeStrictBridgeJSON(body, &input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.CancelHandsInstruction(r.Context(), projectID, parts[3], principal, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	default:
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		writeErrorMessage(w, http.StatusNotFound, "internal workroom route not found")
	}
}

func readBoundedBridgeBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return []byte{}, nil
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, workroomMaxBridgeBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > workroomMaxBridgeBodyBytes {
		return nil, errors.New("request body is too large")
	}
	return payload, nil
}

func decodeStrictBridgeJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func (s *Server) handleArchitectOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	payload, err := s.repo.ArchitectOrders(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
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

func (s *Server) handleOverseerThreads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	payload, err := s.repo.OverseerHistory(ctx)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleOverseerLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var input OverseerLaunchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
		return
	}
	payload, err := s.repo.BuildOverseerLaunchPlan(input)
	if err != nil {
		writeRepoError(w, err)
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

	if len(parts) == 1 || (len(parts) == 2 && parts[1] == "snapshot") {
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
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.ProjectOverview(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "docs":
		if len(parts) == 2 {
			payload, err := s.repo.ProjectDocs(ctx, projectID)
			if err != nil {
				writeRepoError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, payload)
			return
		}
		if len(parts) == 3 && parts[2] == "tree" {
			payload, err := s.repo.ProjectDocsTree(ctx, projectID)
			if err != nil {
				writeRepoError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, payload)
			return
		}
		if len(parts) != 3 {
			writeErrorMessage(w, http.StatusNotFound, "project document route not found")
			return
		}
		docID, err := strconv.Atoi(parts[2])
		if err != nil || docID <= 0 {
			writeErrorMessage(w, http.StatusBadRequest, "invalid project document id")
			return
		}
		payload, err := s.repo.ProjectDoc(ctx, projectID, docID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "backlog":
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.ProjectBacklog(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "netrunners":
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.ProjectNetrunners(ctx, projectID, strings.Split(r.URL.Query().Get("status"), ","))
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "wave-reviews":
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.ProjectWaveReviews(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "waves":
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.ProjectMissionControlWaves(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "fixer-chat-binding":
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.FixerChatBinding(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "overseer-chat-binding":
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.OverseerChatBinding(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "fixer-threads":
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.FixerThreads(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "hands-threads":
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "project route not found")
			return
		}
		payload, err := s.repo.HandsThreads(ctx, projectID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case "skills":
		if len(parts) == 2 {
			payload, err := s.repo.ProjectSkills(ctx, projectID)
			if err != nil {
				writeRepoError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, payload)
			return
		}
		if len(parts) != 4 {
			writeErrorMessage(w, http.StatusNotFound, "project skill route not found")
			return
		}
		payload, err := s.repo.ProjectSkill(ctx, projectID, parts[2], parts[3])
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
	parts := strings.Split(path, "/")
	sessionID, err := strconv.Atoi(parts[0])
	if err != nil || sessionID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid session id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if len(parts) == 1 {
		payload, err := s.repo.NetrunnerDetail(ctx, sessionID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}
	if len(parts) == 2 && parts[1] == "thread" {
		payload, err := s.repo.NetrunnerThread(ctx, sessionID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}
	writeErrorMessage(w, http.StatusNotFound, "session route not found")
}

func (s *Server) handleProjectActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/actions/projects/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		writeErrorMessage(w, http.StatusNotFound, "project action route not found")
		return
	}
	projectID, err := strconv.Atoi(parts[0])
	if err != nil || projectID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid project id")
		return
	}
	timeout := 10 * time.Second
	if len(parts) == 4 && parts[1] == "planned-waves" && parts[3] == "initialize" {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	switch {
	case len(parts) == 4 && parts[1] == "planned-waves" && parts[3] == "initialize":
		planID, err := strconv.Atoi(parts[2])
		if err != nil || planID <= 0 {
			writeErrorMessage(w, http.StatusBadRequest, "invalid planned wave id")
			return
		}
		if err := decodeOptionalJSON(r, &struct{}{}); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.InitializeMissionControlPlannedWave(ctx, projectID, planID)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case len(parts) == 2 && parts[1] == "tasks":
		var input CreateTaskInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.CreateTask(ctx, projectID, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case len(parts) == 2 && parts[1] == "fixer-chats":
		var input FixerChatLaunchInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.LaunchFixerChat(ctx, projectID, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, payload)
	case len(parts) == 3 && parts[1] == "skills":
		var input UpdateManagedSkillInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.UpdateProjectSkill(ctx, projectID, parts[2], input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	default:
		writeErrorMessage(w, http.StatusNotFound, "project action route not found")
	}
}

func (s *Server) handleOrderActions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	for _, prefix := range []string{"/api/actions/orders/", "/api/orders/"} {
		if strings.HasPrefix(path, prefix) {
			path = strings.TrimPrefix(path, prefix)
			break
		}
	}
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || len(parts) > 3 {
		writeErrorMessage(w, http.StatusNotFound, "order action route not found")
		return
	}

	// The original accept endpoint supports both /orders/accept and
	// /orders/{id}/accept. Keep that contract while sharing the route with the
	// explicit sandbox lifecycle endpoints below.
	pathOrderID := 0
	if parts[0] == "accept" {
		if len(parts) != 1 || r.Method != http.MethodPost {
			writeErrorMessage(w, http.StatusNotFound, "order action route not found")
			return
		}
	} else {
		var err error
		pathOrderID, err = strconv.Atoi(parts[0])
		if err != nil || pathOrderID <= 0 {
			writeErrorMessage(w, http.StatusBadRequest, "invalid order id")
			return
		}
		if len(parts) == 1 {
			if r.Method != http.MethodGet {
				writeErrorMessage(w, http.StatusNotFound, "order action route not found")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			payload, err := s.repo.InspectOrderSandbox(ctx, pathOrderID)
			if errors.Is(err, ErrSandboxNotFound) {
				writeErrorMessage(w, http.StatusNotFound, "sandbox not found")
				return
			}
			if err != nil {
				writeRepoError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, payload)
			return
		}
		if len(parts) == 2 && (parts[1] == "sandbox" || parts[1] == "preview" || parts[1] == "preview-url") {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			switch parts[1] {
			case "preview", "preview-url":
				if r.Method != http.MethodGet {
					writeMethodNotAllowed(w)
					return
				}
				payload, err := s.repo.PreviewOrder(ctx, pathOrderID)
				if errors.Is(err, ErrSandboxNotFound) {
					writeErrorMessage(w, http.StatusNotFound, "sandbox not found")
					return
				}
				if err != nil {
					writeRepoError(w, err)
					return
				}
				if payload.Status == "expired" || payload.Status == "removed" {
					writeJSON(w, http.StatusGone, payload)
					return
				}
				writeJSON(w, http.StatusOK, payload)
				return
			}
			switch r.Method {
			case http.MethodGet:
				payload, err := s.repo.InspectOrderSandbox(ctx, pathOrderID)
				if errors.Is(err, ErrSandboxNotFound) {
					writeErrorMessage(w, http.StatusNotFound, "sandbox not found")
					return
				}
				if err != nil {
					writeRepoError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, payload)
				return
			case http.MethodPost:
				var input AcceptOrderInput
				if err := decodeOptionalJSON(r, &input); err != nil {
					writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
					return
				}
				input.OrderID = pathOrderID
				payload, err := s.repo.CreateOrderSandbox(ctx, input)
				if err != nil {
					writeRepoError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, payload)
				return
			case http.MethodDelete:
				var input TeardownSandboxInput
				if err := decodeOptionalJSON(r, &input); err != nil {
					writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
					return
				}
				payload, err := s.repo.TeardownOrderSandbox(ctx, pathOrderID, input)
				if errors.Is(err, ErrSandboxNotFound) {
					writeErrorMessage(w, http.StatusNotFound, "sandbox not found")
					return
				}
				if err != nil {
					writeRepoError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, payload)
				return
			default:
				writeMethodNotAllowed(w)
				return
			}
		}
		if len(parts) == 3 && parts[1] == "sandbox" && parts[2] == "teardown" {
			if r.Method != http.MethodPost && r.Method != http.MethodDelete {
				writeMethodNotAllowed(w)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			var input TeardownSandboxInput
			if err := decodeOptionalJSON(r, &input); err != nil {
				writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
				return
			}
			payload, err := s.repo.TeardownOrderSandbox(ctx, pathOrderID, input)
			if errors.Is(err, ErrSandboxNotFound) {
				writeErrorMessage(w, http.StatusNotFound, "sandbox not found")
				return
			}
			if err != nil {
				writeRepoError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, payload)
			return
		}
		if len(parts) == 2 && parts[1] == "accept" {
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w)
				return
			}
		} else {
			writeErrorMessage(w, http.StatusNotFound, "order action route not found")
			return
		}
	}

	var input AcceptOrderInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if pathOrderID > 0 {
		if input.OrderID > 0 && input.OrderID != pathOrderID {
			writeErrorMessage(w, http.StatusBadRequest, "order id does not match route")
			return
		}
		input.OrderID = pathOrderID
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	payload, err := s.repo.AcceptOrder(ctx, input)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func decodeOptionalJSON(r *http.Request, target any) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	token := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/previews/"), "/")
	if token == "" || strings.Contains(token, "/") {
		writeErrorMessage(w, http.StatusBadRequest, "invalid preview token")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	payload, err := s.repo.previewByToken(ctx, token)
	if errors.Is(err, ErrPreviewNotFound) {
		writeErrorMessage(w, http.StatusNotFound, "preview not found")
		return
	}
	if err != nil {
		writeRepoError(w, err)
		return
	}
	if payload.Status == "expired" || payload.Status == "removed" {
		writeJSON(w, http.StatusGone, payload)
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
	if len(parts) < 2 {
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
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "session action route not found")
			return
		}
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
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "session action route not found")
			return
		}
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
		if len(parts) != 2 {
			writeErrorMessage(w, http.StatusNotFound, "session action route not found")
			return
		}
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
	case "thread":
		if len(parts) != 3 || parts[2] != "messages" {
			writeErrorMessage(w, http.StatusNotFound, "session action route not found")
			return
		}
		var input ContinueNetrunnerThreadInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payload, err := s.repo.ContinueNetrunnerThread(ctx, sessionID, input)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, payload)
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
	if errors.Is(err, ErrWorkroomAuthorization) {
		writeErrorMessage(w, http.StatusForbidden, "workroom capability denied")
		return
	}
	if errors.Is(err, ErrPlannedWaveInitializeUnavailable) {
		writeErrorMessage(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if errors.Is(err, ErrPlannedWaveInitializeRejected) {
		writeErrorMessage(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, ErrSandboxConflict) {
		writeErrorMessage(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorMessage(w, http.StatusNotFound, "record not found")
		return
	}
	if errors.Is(err, ErrSkillNotFound) {
		writeErrorMessage(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, ErrInvalidSkillPath) {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "invalid "),
		strings.Contains(message, "unknown "),
		strings.Contains(message, "not allowed"),
		strings.Contains(message, "required"),
		strings.Contains(message, "must be "),
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
