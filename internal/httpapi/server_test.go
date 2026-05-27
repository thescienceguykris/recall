package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"recall/internal/jobs"
	"recall/internal/store"
)

type fakeRunner struct{}

func (fakeRunner) Run(ctx context.Context, req jobs.Request) (jobs.Result, error) {
	return jobs.Result{JobID: "job1", NoteID: "note1", TranscriptID: "transcript1", NoteURL: "/artefacts/note1", TranscriptURL: "/artefacts/transcript1"}, nil
}

func (fakeRunner) Start(ctx context.Context, req jobs.Request) (jobs.Result, error) {
	return jobs.Result{JobID: "job1", StatusURL: "/api/jobs/job1"}, nil
}

func (fakeRunner) StartResummarise(ctx context.Context, jobID string) (jobs.Result, error) {
	if jobID == "missing" {
		return jobs.Result{}, errJobNotFound{}
	}
	return jobs.Result{JobID: jobID, NoteID: "note1", TranscriptID: "transcript1", StatusURL: "/api/jobs/" + jobID}, nil
}

func (fakeRunner) StartRetranscribe(ctx context.Context, jobID string) (jobs.Result, error) {
	if jobID == "missing" {
		return jobs.Result{}, errJobNotFound{}
	}
	return jobs.Result{JobID: jobID, NoteID: "note1", TranscriptID: "transcript1", StatusURL: "/api/jobs/" + jobID}, nil
}

type errJobNotFound struct{}

func (errJobNotFound) Error() string { return "job not found" }

type fakeStore struct{}

func (fakeStore) ListJobs(ctx context.Context) ([]store.Job, error) {
	return []store.Job{{ID: "job1", SourceURL: "https://example.com", Status: "complete", Stage: "complete", Message: "Complete", NoteID: "note1", TranscriptID: "transcript1", Created: time.Now()}}, nil
}

func (fakeStore) GetJob(ctx context.Context, id string) (store.Job, bool, error) {
	return store.Job{ID: id, SourceURL: "https://example.com"}, true, nil
}

func (fakeStore) GetArtefact(ctx context.Context, id string) (store.Artefact, bool, error) {
	return store.Artefact{ID: id, Name: "note.md", MediaType: "text/markdown", Content: "# Note"}, true, nil
}

func (fakeStore) DeleteJob(ctx context.Context, id string) (bool, error) {
	return id == "job1", nil
}

type rejectingAuth struct{}

func (rejectingAuth) Enabled() bool { return true }
func (rejectingAuth) CurrentUser(r *http.Request) (store.User, bool) {
	return store.User{}, false
}
func (rejectingAuth) Login(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
func (rejectingAuth) Callback(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
func (rejectingAuth) Logout(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func TestValidateURL(t *testing.T) {
	valid := []string{"https://example.com/video", "http://example.com"}
	for _, raw := range valid {
		if err := ValidateURL(raw); err != nil {
			t.Fatalf("ValidateURL(%q) error = %v", raw, err)
		}
	}
	invalid := []string{"", "ftp://example.com", "https://", "notaurl", "https://example.com\nbad"}
	for _, raw := range invalid {
		if err := ValidateURL(raw); err == nil {
			t.Fatalf("ValidateURL(%q) expected error", raw)
		}
	}
}

func TestHealthz(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestUIRoute(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Recall") {
		t.Fatalf("unexpected UI response %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateBriefingValidation(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/briefings", strings.NewReader(`{"url":"ftp://example.com"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCreateBriefingSuccess(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/briefings", strings.NewReader(`{"url":"https://example.com","tags":["cyber"]}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var result jobs.Result
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.StatusURL != "/api/jobs/job1" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProtectedRoutesRequireAuthWhenEnabled(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}, Auth: rejectingAuth{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestArtefactInline(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/artefacts/note1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "inline") {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestDeleteJob(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/jobs/job1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRedoSummary(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/jobs/job1/resummarise", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRedoTranscript(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/jobs/job1/retranscribe", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteJobNotFound(t *testing.T) {
	handler := Server{Runner: fakeRunner{}, Store: fakeStore{}}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/jobs/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
