package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"recall/internal/auth"
	"recall/internal/jobs"
	"recall/internal/store"
)

//go:embed static/*
var staticFS embed.FS

type JobRunner interface {
	Run(ctx context.Context, req jobs.Request) (jobs.Result, error)
	Start(ctx context.Context, req jobs.Request) (jobs.Result, error)
	StartResummarise(ctx context.Context, jobID string) (jobs.Result, error)
	StartRetranscribe(ctx context.Context, jobID string) (jobs.Result, error)
}

type Store interface {
	ListJobs(ctx context.Context) ([]store.Job, error)
	GetJob(ctx context.Context, id string) (store.Job, bool, error)
	GetArtefact(ctx context.Context, id string) (store.Artefact, bool, error)
	DeleteJob(ctx context.Context, id string) (bool, error)
}

type Server struct {
	Runner     JobRunner
	Store      Store
	Auth       auth.Service
	JobTimeout time.Duration
}

func (s Server) Handler() http.Handler {
	authSvc := s.auth()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("GET /auth/login", authSvc.Login)
	mux.HandleFunc("GET /auth/callback", authSvc.Callback)
	mux.HandleFunc("POST /auth/logout", authSvc.Logout)
	mux.Handle("POST /briefings", s.requireAuth(http.HandlerFunc(s.createBriefing)))
	mux.Handle("GET /api/jobs", s.requireAuth(http.HandlerFunc(s.listJobs)))
	mux.Handle("GET /api/jobs/", s.requireAuth(http.HandlerFunc(s.getJob)))
	mux.Handle("POST /api/jobs/", s.requireAuth(http.HandlerFunc(s.jobAction)))
	mux.Handle("DELETE /api/jobs/", s.requireAuth(http.HandlerFunc(s.deleteJob)))
	mux.Handle("GET /artefacts/", s.requireAuth(http.HandlerFunc(s.getArtefact)))

	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	return mux
}

func (s Server) auth() auth.Service {
	if s.Auth == nil {
		return auth.Noop{}
	}
	return s.Auth
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s Server) me(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth().CurrentUser(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_enabled":  s.auth().Enabled(),
		"authenticated": ok,
		"user":          user,
	})
}

func (s Server) createBriefing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL  string   `json:"url"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}
	if err := ValidateURL(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if s.JobTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.JobTimeout)
		defer cancel()
	}
	result, err := s.Runner.Start(ctx, jobs.Request{URL: strings.TrimSpace(req.URL), Tags: req.Tags})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s Server) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.Store.ListJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s Server) getJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	job, ok, err := s.Store.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s Server) jobAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	jobID, action, ok := strings.Cut(path, "/")
	if !ok || jobID == "" {
		writeError(w, http.StatusNotFound, "job action not found")
		return
	}
	ctx := r.Context()
	if s.JobTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.JobTimeout)
		defer cancel()
	}
	var (
		result jobs.Result
		err    error
	)
	switch action {
	case "resummarise":
		result, err = s.Runner.StartResummarise(ctx, jobID)
	case "retranscribe":
		result, err = s.Runner.StartRetranscribe(ctx, jobID)
	default:
		writeError(w, http.StatusNotFound, "job action not found")
		return
	}
	if err != nil {
		if err.Error() == "job not found" {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s Server) deleteJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	deleted, err := s.Store.DeleteJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s Server) getArtefact(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/artefacts/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "invalid artefact id")
		return
	}
	artefact, ok, err := s.Store.GetArtefact(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "artefact not found")
		return
	}
	w.Header().Set("Content-Type", artefact.MediaType+"; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(artefact.Name, `"`, "")+`"`)
	_, _ = w.Write([]byte(artefact.Content))
}

func ValidateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("url is required")
	}
	if strings.ContainsAny(raw, "\x00\r\n\t") {
		return errors.New("url contains unsafe characters")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return errors.New("url must be valid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("url must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("url must include a host")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.auth().CurrentUser(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(store.WithUserID(r.Context(), user.ID)))
	})
}
