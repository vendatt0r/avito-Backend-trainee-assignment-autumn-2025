package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"Backend-trainee-assignment-autumn-2025/internal/models"
	"Backend-trainee-assignment-autumn-2025/internal/storage"
)

type Server struct {
	store *storage.Store
}

func RegisterHandlers(mux *http.ServeMux, st *storage.Store) {
	s := &Server{store: st}

	mux.HandleFunc("/team/add", s.handleTeamAdd)
	mux.HandleFunc("/team/get", s.handleTeamGet)
	mux.HandleFunc("/users/setIsActive", s.handleSetIsActive)
	mux.HandleFunc("/pullRequest/create", s.handleCreatePR)
	mux.HandleFunc("/pullRequest/merge", s.handleMergePR)
	mux.HandleFunc("/pullRequest/reassign", s.handleReassign)
	mux.HandleFunc("/users/getReview", s.handleGetReview)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/team/deactivateUsers", s.handleDeactivateUsers)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, httpCode int, code, msg string) {
	var e models.ErrorResponse
	e.Error.Code = code
	e.Error.Message = msg
	writeJSON(w, httpCode, e)
}

func (s *Server) handleTeamAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var t models.Team
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, 400, "INVALID", "bad request")
		return
	}
	if t.TeamName == "" {
		writeError(w, 400, "INVALID", "team_name required")
		return
	}
	if err := s.store.UpsertTeam(context.Background(), t); err != nil {
		writeError(w, 500, "ERROR", err.Error())
		return
	}
	writeJSON(w, 201, map[string]models.Team{"team": t})
}

func (s *Server) handleTeamGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	tn := r.URL.Query().Get("team_name")
	if tn == "" {
		writeError(w, 400, "INVALID", "team_name required")
		return
	}
	t, err := s.store.GetTeam(context.Background(), tn)
	if err != nil {
		if err == storage.ErrTeamNotFound {
			writeError(w, 404, "NOT_FOUND", "team not found")
			return
		}
		writeError(w, 500, "ERROR", err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) handleSetIsActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var body struct {
		UserID   string `json:"user_id"`
		IsActive bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID", "bad request")
		return
	}
	u, err := s.store.SetUserActive(context.Background(), body.UserID, body.IsActive)
	if err != nil {
		if err == storage.ErrUserNotFound {
			writeError(w, 404, "NOT_FOUND", "user not found")
			return
		}
		writeError(w, 500, "ERROR", err.Error())
		return
	}
	writeJSON(w, 200, map[string]models.User{"user": u})
}

func (s *Server) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var body struct {
		ID     string `json:"pull_request_id"`
		Name   string `json:"pull_request_name"`
		Author string `json:"author_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID", "bad request")
		return
	}
	pr := models.PullRequest{
		PullRequestID:   body.ID,
		PullRequestName: body.Name,
		AuthorID:        body.Author,
	}
	created, err := s.store.CreatePR(context.Background(), pr)
	if err != nil {
		if err == storage.ErrPRExists {
			writeError(w, 409, "PR_EXISTS", "PR id already exists")
			return
		}
		if err == storage.ErrUserNotFound {
			writeError(w, 404, "NOT_FOUND", "author/team not found")
			return
		}
		writeError(w, 500, "ERROR", err.Error())
		return
	}
	writeJSON(w, 201, map[string]models.PullRequest{"pr": created})
}

func (s *Server) handleMergePR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var body struct {
		ID string `json:"pull_request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID", "bad request")
		return
	}
	pr, err := s.store.MergePR(context.Background(), body.ID)
	if err != nil {
		if err == storage.ErrPRNotFound {
			writeError(w, 404, "NOT_FOUND", "PR not found")
			return
		}
		writeError(w, 500, "ERROR", err.Error())
		return
	}
	writeJSON(w, 200, map[string]models.PullRequest{"pr": pr})
}

func (s *Server) handleReassign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var body struct {
		ID    string `json:"pull_request_id"`
		OldID string `json:"old_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID", "bad request")
		return
	}
	pr, newID, err := s.store.ReassignReviewer(context.Background(), body.ID, body.OldID)
	if err != nil {
		switch err {
		case storage.ErrPRNotFound:
			writeError(w, 404, "NOT_FOUND", "PR not found")
		case storage.ErrPRMerged:
			writeError(w, 409, "PR_MERGED", "cannot reassign on merged PR")
		case storage.ErrNotAssigned:
			writeError(w, 409, "NOT_ASSIGNED", "reviewer is not assigned to this PR")
		case storage.ErrNoCandidate:
			writeError(w, 409, "NO_CANDIDATE", "no active replacement candidate in team")
		default:
			msg := err.Error()
			if strings.Contains(msg, "user not found") {
				writeError(w, 404, "NOT_FOUND", "user not found")
			} else {
				writeError(w, 500, "ERROR", msg)
			}
		}
		return
	}
	writeJSON(w, 200, map[string]interface{}{"pr": pr, "replaced_by": newID})
}

func (s *Server) handleGetReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	uid := r.URL.Query().Get("user_id")
	if uid == "" {
		writeError(w, 400, "INVALID", "user_id required")
		return
	}
	prs, err := s.store.GetPRsForReviewer(context.Background(), uid)
	if err != nil {
		writeError(w, 500, "ERROR", err.Error())
		return
	}
	resp := map[string]interface{}{
		"user_id":       uid,
		"pull_requests": prs,
	}
	writeJSON(w, 200, resp)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}

	userStats, err := s.store.GetReviewerStats(context.Background())
	if err != nil {
		writeError(w, 500, "ERROR", err.Error())
		return
	}

	prStats, err := s.store.GetPRStats(context.Background())
	if err != nil {
		writeError(w, 500, "ERROR", err.Error())
		return
	}

	resp := map[string]interface{}{
		"reviewer_assignments": userStats,
		"pr_assignments":       prStats,
	}

	writeJSON(w, 200, resp)
}

func (s *Server) handleDeactivateUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}

	var body struct {
		TeamName string   `json:"team_name"`
		UserIDs  []string `json:"user_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID", "bad request")
		return
	}

	if body.TeamName == "" || len(body.UserIDs) == 0 {
		writeError(w, 400, "INVALID", "team_name and user_ids required")
		return
	}

	err := s.store.BulkDeactivateUsers(context.Background(), body.TeamName, body.UserIDs)
	if err != nil {
		writeError(w, 500, "ERROR", err.Error())
		return
	}

	writeJSON(w, 200, map[string]string{
		"status": "ok",
	})
}
