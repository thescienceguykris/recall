package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"recall/internal/downloader"
	"recall/internal/notes"
	"recall/internal/sanitise"
	"recall/internal/store"
	"recall/internal/summariser"
	"recall/internal/transcriber"
	transcriptclean "recall/internal/transcript"
)

type Store interface {
	UpsertJob(ctx context.Context, job store.Job) error
	AddArtefact(ctx context.Context, artefact store.Artefact) error
	UpsertArtefact(ctx context.Context, artefact store.Artefact) error
	GetJob(ctx context.Context, id string) (store.Job, bool, error)
	GetArtefact(ctx context.Context, id string) (store.Artefact, bool, error)
}

type Runner struct {
	DataDir     string
	Downloader  downloader.Downloader
	Transcriber transcriber.Transcriber
	Summariser  summariser.Summariser
	Store       Store
	Now         func() time.Time
	NewID       func() string
}

type Request struct {
	URL  string
	Tags []string
}

type Result struct {
	JobID         string `json:"job_id"`
	NoteID        string `json:"note_id"`
	TranscriptID  string `json:"transcript_id"`
	NoteURL       string `json:"note_url"`
	TranscriptURL string `json:"transcript_url"`
	StatusURL     string `json:"status_url"`
}

func (r Runner) Start(ctx context.Context, req Request) (Result, error) {
	now := r.now()
	jobID := r.newID()
	ownerID := store.UserIDFromContext(ctx)
	workDir := filepath.Join(r.DataDir, "jobs", jobID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return Result{}, err
	}

	job := store.Job{
		ID: jobID, OwnerID: ownerID, SourceURL: req.URL, Tags: cleanTags(req.Tags),
		Status: "running", Stage: "queued", Message: "Queued",
		Created: now, Updated: now,
	}
	if err := r.Store.UpsertJob(ctx, job); err != nil {
		return Result{}, err
	}

	go func() {
		_, _ = r.runWithJob(store.WithUserID(context.Background(), ownerID), req, job, workDir)
	}()

	return Result{JobID: jobID, StatusURL: "/api/jobs/" + jobID}, nil
}

func (r Runner) StartResummarise(ctx context.Context, jobID string) (Result, error) {
	ownerID := store.UserIDFromContext(ctx)
	job, ok, err := r.Store.GetJob(ctx, jobID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, fmt.Errorf("job not found")
	}
	if job.Status == "running" {
		return Result{}, fmt.Errorf("job is already running")
	}
	if job.TranscriptID == "" {
		return Result{}, fmt.Errorf("job has no transcript")
	}
	if job.NoteID == "" {
		job.NoteID = job.ID + "-note"
	}
	job.Status = "running"
	job.Stage = "summarising"
	job.Message = "Redoing briefing"
	job.Error = ""
	job.Updated = r.now()
	if err := r.Store.UpsertJob(ctx, job); err != nil {
		return Result{}, err
	}

	go func() {
		_, _ = r.resummarise(store.WithUserID(context.Background(), ownerID), job)
	}()

	return Result{JobID: job.ID, NoteID: job.NoteID, TranscriptID: job.TranscriptID, NoteURL: "/artefacts/" + job.NoteID, TranscriptURL: "/artefacts/" + job.TranscriptID, StatusURL: "/api/jobs/" + job.ID}, nil
}

func (r Runner) StartRetranscribe(ctx context.Context, jobID string) (Result, error) {
	ownerID := store.UserIDFromContext(ctx)
	job, ok, err := r.Store.GetJob(ctx, jobID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, fmt.Errorf("job not found")
	}
	if job.Status == "running" {
		return Result{}, fmt.Errorf("job is already running")
	}
	if job.SourceURL == "" {
		return Result{}, fmt.Errorf("job has no source URL")
	}
	if job.TranscriptID == "" {
		job.TranscriptID = job.ID + "-transcript"
	}
	if job.NoteID == "" {
		job.NoteID = job.ID + "-note"
	}
	job.Status = "running"
	job.Stage = "downloading"
	job.Message = "Redoing transcript"
	job.Error = ""
	job.Updated = r.now()
	if err := r.Store.UpsertJob(ctx, job); err != nil {
		return Result{}, err
	}

	go func() {
		_, _ = r.retranscribe(store.WithUserID(context.Background(), ownerID), job)
	}()

	return Result{JobID: job.ID, NoteID: job.NoteID, TranscriptID: job.TranscriptID, NoteURL: "/artefacts/" + job.NoteID, TranscriptURL: "/artefacts/" + job.TranscriptID, StatusURL: "/api/jobs/" + job.ID}, nil
}

func (r Runner) Run(ctx context.Context, req Request) (Result, error) {
	now := r.now()
	jobID := r.newID()
	ownerID := store.UserIDFromContext(ctx)
	workDir := filepath.Join(r.DataDir, "jobs", jobID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return Result{}, err
	}

	job := store.Job{
		ID: jobID, OwnerID: ownerID, SourceURL: req.URL, Tags: cleanTags(req.Tags),
		Status: "running", Stage: "queued", Message: "Queued",
		Created: now, Updated: now,
	}
	if err := r.Store.UpsertJob(ctx, job); err != nil {
		return Result{}, err
	}
	return r.runWithJob(ctx, req, job, workDir)
}

func (r Runner) runWithJob(ctx context.Context, req Request, job store.Job, workDir string) (Result, error) {
	fail := func(err error) (Result, error) {
		job.Status = "failed"
		job.Stage = "failed"
		job.Message = "Failed"
		job.Error = err.Error()
		job.Updated = r.now()
		_ = r.Store.UpsertJob(store.WithUserID(context.Background(), job.OwnerID), job)
		return Result{}, err
	}

	if err := r.updateStage(ctx, &job, "downloading", "Downloading video and extracting audio"); err != nil {
		return fail(err)
	}
	download, err := r.Downloader.DownloadAudio(ctx, req.URL, workDir)
	if err != nil {
		return fail(err)
	}
	job.Title = download.Title
	if err := r.updateStage(ctx, &job, "downloaded", "Audio extracted"); err != nil {
		return fail(err)
	}

	if err := r.updateStage(ctx, &job, "transcribing", "Transcribing audio"); err != nil {
		return fail(err)
	}
	transcript, err := r.Transcriber.Transcribe(ctx, download.AudioPath)
	if err != nil {
		return fail(err)
	}
	transcript = transcriptclean.CleanResult(transcript)
	transcriptText := transcriptclean.Paragraphs(transcript)
	if err := r.updateStage(ctx, &job, "transcribed", "Transcript generated"); err != nil {
		return fail(err)
	}

	if err := r.updateStage(ctx, &job, "summarising", "Creating briefing"); err != nil {
		return fail(err)
	}
	briefing, err := r.Summariser.Summarise(ctx, summariser.SummaryInput{
		Title:      download.Title,
		SourceURL:  download.SourceURL,
		Transcript: transcriptText,
		Tags:       job.Tags,
		WorkDir:    workDir,
	})
	if err != nil {
		return fail(err)
	}
	if len(briefing.SuggestedTags) > 0 {
		job.Tags = mergeTags(job.Tags, briefing.SuggestedTags)
	}

	if err := r.updateStage(ctx, &job, "saving", "Saving note and transcript"); err != nil {
		return fail(err)
	}
	title := firstNonEmpty(briefing.Title, download.Title, "Video briefing")
	transcriptID := job.ID + "-transcript"
	noteID := job.ID + "-note"
	created := r.now()
	transcriptName := sanitise.Filename(title+" transcript", "transcript") + ".md"
	transcriptMarkdown := notes.RenderTranscript(title+" Transcript", download.SourceURL, transcriptText, created)
	if err := r.Store.AddArtefact(ctx, store.Artefact{
		ID: transcriptID, OwnerID: job.OwnerID, JobID: job.ID, Name: transcriptName, Kind: "transcript", MediaType: "text/markdown", Content: transcriptMarkdown, Created: created,
	}); err != nil {
		return fail(err)
	}

	noteName := sanitise.Filename(title, "briefing") + ".md"
	noteMarkdown := notes.RenderNote(notes.NoteInput{
		SourceURL: download.SourceURL, Created: created, Tags: job.Tags, TranscriptID: transcriptID, TranscriptName: transcriptName, Briefing: briefing,
	})
	if err := r.Store.AddArtefact(ctx, store.Artefact{
		ID: noteID, OwnerID: job.OwnerID, JobID: job.ID, Name: noteName, Kind: "note", MediaType: "text/markdown", Content: noteMarkdown, Created: created,
	}); err != nil {
		return fail(err)
	}

	job.Status = "complete"
	job.Stage = "complete"
	job.Message = "Complete"
	job.NoteID = noteID
	job.TranscriptID = transcriptID
	job.Updated = r.now()
	if err := r.Store.UpsertJob(ctx, job); err != nil {
		return fail(err)
	}

	return Result{
		JobID: job.ID, NoteID: noteID, TranscriptID: transcriptID,
		NoteURL: "/artefacts/" + noteID, TranscriptURL: "/artefacts/" + transcriptID, StatusURL: "/api/jobs/" + job.ID,
	}, nil
}

func (r Runner) resummarise(ctx context.Context, job store.Job) (Result, error) {
	fail := func(err error) (Result, error) {
		job.Status = "failed"
		job.Stage = "failed"
		job.Message = "Failed"
		job.Error = err.Error()
		job.Updated = r.now()
		_ = r.Store.UpsertJob(store.WithUserID(context.Background(), job.OwnerID), job)
		return Result{}, err
	}

	transcriptArtefact, ok, err := r.Store.GetArtefact(ctx, job.TranscriptID)
	if err != nil {
		return fail(err)
	}
	if !ok {
		return fail(fmt.Errorf("transcript not found"))
	}

	transcriptText := transcriptFromMarkdown(transcriptArtefact.Content)
	workDir := filepath.Join(r.DataDir, "jobs", job.ID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fail(err)
	}

	if err := r.updateStage(ctx, &job, "summarising", "Redoing briefing"); err != nil {
		return fail(err)
	}
	briefing, err := r.Summariser.Summarise(ctx, summariser.SummaryInput{
		Title:      job.Title,
		SourceURL:  job.SourceURL,
		Transcript: transcriptText,
		Tags:       job.Tags,
		WorkDir:    workDir,
	})
	if err != nil {
		return fail(err)
	}
	if len(briefing.SuggestedTags) > 0 {
		job.Tags = mergeTags(job.Tags, briefing.SuggestedTags)
	}

	if err := r.updateStage(ctx, &job, "saving", "Saving redone note"); err != nil {
		return fail(err)
	}
	title := firstNonEmpty(briefing.Title, job.Title, "Video briefing")
	noteMarkdown := notes.RenderNote(notes.NoteInput{
		SourceURL: job.SourceURL, Created: r.now(), Tags: job.Tags, TranscriptID: job.TranscriptID, TranscriptName: transcriptArtefact.Name, Briefing: briefing,
	})
	noteID := job.NoteID
	if noteID == "" {
		noteID = job.ID + "-note"
	}
	if err := r.Store.UpsertArtefact(ctx, store.Artefact{
		ID: noteID, OwnerID: job.OwnerID, JobID: job.ID, Name: sanitise.Filename(title, "briefing") + ".md", Kind: "note", MediaType: "text/markdown", Content: noteMarkdown, Created: r.now(), Updated: r.now(),
	}); err != nil {
		return fail(err)
	}

	job.Status = "complete"
	job.Stage = "complete"
	job.Message = "Briefing redone"
	job.NoteID = noteID
	job.Updated = r.now()
	if err := r.Store.UpsertJob(ctx, job); err != nil {
		return fail(err)
	}
	return Result{JobID: job.ID, NoteID: noteID, TranscriptID: job.TranscriptID, NoteURL: "/artefacts/" + noteID, TranscriptURL: "/artefacts/" + job.TranscriptID, StatusURL: "/api/jobs/" + job.ID}, nil
}

func (r Runner) retranscribe(ctx context.Context, job store.Job) (Result, error) {
	fail := func(err error) (Result, error) {
		job.Status = "failed"
		job.Stage = "failed"
		job.Message = "Failed"
		job.Error = err.Error()
		job.Updated = r.now()
		_ = r.Store.UpsertJob(store.WithUserID(context.Background(), job.OwnerID), job)
		return Result{}, err
	}

	workDir := filepath.Join(r.DataDir, "jobs", job.ID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fail(err)
	}

	if err := r.updateStage(ctx, &job, "downloading", "Re-downloading video and extracting audio"); err != nil {
		return fail(err)
	}
	download, err := r.Downloader.DownloadAudio(ctx, job.SourceURL, workDir)
	if err != nil {
		return fail(err)
	}
	if strings.TrimSpace(download.Title) != "" {
		job.Title = download.Title
	}
	sourceURL := firstNonEmpty(download.SourceURL, job.SourceURL)
	job.SourceURL = sourceURL
	if err := r.updateStage(ctx, &job, "downloaded", "Audio extracted"); err != nil {
		return fail(err)
	}

	if err := r.updateStage(ctx, &job, "transcribing", "Redoing transcript"); err != nil {
		return fail(err)
	}
	transcript, err := r.Transcriber.Transcribe(ctx, download.AudioPath)
	if err != nil {
		return fail(err)
	}
	transcript = transcriptclean.CleanResult(transcript)
	transcriptText := transcriptclean.Paragraphs(transcript)
	if err := r.updateStage(ctx, &job, "transcribed", "Transcript redone"); err != nil {
		return fail(err)
	}

	if err := r.updateStage(ctx, &job, "summarising", "Creating briefing from redone transcript"); err != nil {
		return fail(err)
	}
	briefing, err := r.Summariser.Summarise(ctx, summariser.SummaryInput{
		Title:      job.Title,
		SourceURL:  sourceURL,
		Transcript: transcriptText,
		Tags:       job.Tags,
		WorkDir:    workDir,
	})
	if err != nil {
		return fail(err)
	}
	if len(briefing.SuggestedTags) > 0 {
		job.Tags = mergeTags(job.Tags, briefing.SuggestedTags)
	}

	if err := r.updateStage(ctx, &job, "saving", "Saving redone transcript and note"); err != nil {
		return fail(err)
	}
	title := firstNonEmpty(briefing.Title, job.Title, "Video briefing")
	now := r.now()
	transcriptID := job.TranscriptID
	if transcriptID == "" {
		transcriptID = job.ID + "-transcript"
	}
	transcriptName := sanitise.Filename(title+" transcript", "transcript") + ".md"
	transcriptMarkdown := notes.RenderTranscript(title+" Transcript", sourceURL, transcriptText, now)
	if err := r.Store.UpsertArtefact(ctx, store.Artefact{
		ID: transcriptID, OwnerID: job.OwnerID, JobID: job.ID, Name: transcriptName, Kind: "transcript", MediaType: "text/markdown", Content: transcriptMarkdown, Created: now, Updated: now,
	}); err != nil {
		return fail(err)
	}

	noteID := job.NoteID
	if noteID == "" {
		noteID = job.ID + "-note"
	}
	noteMarkdown := notes.RenderNote(notes.NoteInput{
		SourceURL: sourceURL, Created: now, Tags: job.Tags, TranscriptID: transcriptID, TranscriptName: transcriptName, Briefing: briefing,
	})
	if err := r.Store.UpsertArtefact(ctx, store.Artefact{
		ID: noteID, OwnerID: job.OwnerID, JobID: job.ID, Name: sanitise.Filename(title, "briefing") + ".md", Kind: "note", MediaType: "text/markdown", Content: noteMarkdown, Created: now, Updated: now,
	}); err != nil {
		return fail(err)
	}

	job.Status = "complete"
	job.Stage = "complete"
	job.Message = "Transcript and briefing redone"
	job.NoteID = noteID
	job.TranscriptID = transcriptID
	job.Updated = r.now()
	if err := r.Store.UpsertJob(ctx, job); err != nil {
		return fail(err)
	}
	return Result{JobID: job.ID, NoteID: noteID, TranscriptID: transcriptID, NoteURL: "/artefacts/" + noteID, TranscriptURL: "/artefacts/" + transcriptID, StatusURL: "/api/jobs/" + job.ID}, nil
}

func (r Runner) updateStage(ctx context.Context, job *store.Job, stage, message string) error {
	job.Stage = stage
	job.Message = message
	job.Updated = r.now()
	return r.Store.UpsertJob(ctx, *job)
}

func (r Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r Runner) newID() string {
	if r.NewID != nil {
		return r.NewID()
	}
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

func cleanTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if !seen[key] {
			out = append(out, tag)
			seen[key] = true
		}
	}
	return out
}

func mergeTags(a, b []string) []string {
	return cleanTags(append(a, b...))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func transcriptFromMarkdown(markdown string) string {
	markdown = strings.TrimSpace(markdown)
	if strings.HasPrefix(markdown, "---\n") {
		rest := markdown[4:]
		if idx := strings.Index(rest, "\n---"); idx >= 0 {
			markdown = strings.TrimSpace(rest[idx+4:])
		}
	}
	if strings.HasPrefix(markdown, "# ") {
		if idx := strings.Index(markdown, "\n\n"); idx >= 0 {
			markdown = strings.TrimSpace(markdown[idx+2:])
		}
	}
	return markdown
}
