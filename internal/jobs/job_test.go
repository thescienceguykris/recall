package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"recall/internal/downloader"
	"recall/internal/store"
	"recall/internal/summariser"
	"recall/internal/transcript"
)

type fakeDownloader struct {
	audioPath string
}

func (f fakeDownloader) DownloadAudio(ctx context.Context, url string, workDir string) (downloader.DownloadResult, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return downloader.DownloadResult{}, err
	}
	audio := filepath.Join(workDir, "audio.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o600); err != nil {
		return downloader.DownloadResult{}, err
	}
	return downloader.DownloadResult{AudioPath: audio, Title: "Video Title", SourceURL: url}, nil
}

type fakeTranscriber struct{}

func (fakeTranscriber) Transcribe(ctx context.Context, audioPath string) (transcript.Result, error) {
	return transcript.Result{
		Text: "Transcript text",
		Words: []transcript.Word{
			{Start: 0, End: 0.3, Text: "Transcript."},
			{Start: 2.4, End: 2.7, Text: "text"},
		},
	}, nil
}

type fakeSummariser struct{}

func (fakeSummariser) Summarise(ctx context.Context, input summariser.SummaryInput) (summariser.Briefing, error) {
	return summariser.Briefing{
		Title:              "Briefing Title",
		Summary:            "Summary",
		KeyPoints:          []string{"Point"},
		PracticalTakeaways: []string{"Takeaway"},
		Questions:          []string{"Question?"},
		SuggestedTags:      []string{"suggested"},
	}, nil
}

func TestRunnerRunStoresArtefacts(t *testing.T) {
	db := store.New(filepath.Join(t.TempDir(), "db.json"))
	if err := db.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		DataDir:     t.TempDir(),
		Downloader:  fakeDownloader{},
		Transcriber: fakeTranscriber{},
		Summariser:  fakeSummariser{},
		Store:       db,
		Now:         func() time.Time { return time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC) },
		NewID:       func() string { return "job1" },
	}
	result, err := runner.Run(context.Background(), Request{URL: "https://example.com/video", Tags: []string{"cyber"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.JobID != "job1" || result.NoteID == "" || result.TranscriptID == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	job, ok, err := db.GetJob(context.Background(), "job1")
	if err != nil || !ok {
		t.Fatalf("job not found: %v", err)
	}
	if job.Status != "complete" || job.NoteID == "" || job.TranscriptID == "" {
		t.Fatalf("unexpected job: %+v", job)
	}
	if job.Stage != "complete" || job.Message != "Complete" {
		t.Fatalf("unexpected progress: %+v", job)
	}
	note, ok, err := db.GetArtefact(context.Background(), job.NoteID)
	if err != nil || !ok {
		t.Fatalf("note not found: %v", err)
	}
	if note.Kind != "note" || note.Content == "" {
		t.Fatalf("unexpected note: %+v", note)
	}
	transcriptArtefact, ok, err := db.GetArtefact(context.Background(), job.TranscriptID)
	if err != nil || !ok {
		t.Fatalf("transcript not found: %v", err)
	}
	if transcriptArtefact.Content == "" || !strings.Contains(transcriptArtefact.Content, "[00:00] Transcript") || !strings.Contains(transcriptArtefact.Content, "[00:02] text") {
		t.Fatalf("unexpected transcript: %s", transcriptArtefact.Content)
	}
	if _, err := os.Stat(filepath.Join(runner.DataDir, "jobs", "job1")); err != nil {
		t.Fatalf("work dir missing: %v", err)
	}
}

func TestRunnerResummariseOverwritesNoteFromStoredTranscript(t *testing.T) {
	db := store.New(filepath.Join(t.TempDir(), "db.json"))
	if err := db.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	job := store.Job{
		ID: "job1", SourceURL: "https://example.com/video", Title: "Original", Tags: []string{"cyber"},
		Status: "complete", Stage: "complete", NoteID: "job1-note", TranscriptID: "job1-transcript", Created: now, Updated: now,
	}
	if err := db.UpsertJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := db.AddArtefact(context.Background(), store.Artefact{
		ID: "job1-transcript", JobID: "job1", Name: "transcript.md", Kind: "transcript", MediaType: "text/markdown",
		Content: notesFixture("Stored transcript text."), Created: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddArtefact(context.Background(), store.Artefact{
		ID: "job1-note", JobID: "job1", Name: "old.md", Kind: "note", MediaType: "text/markdown", Content: "old note", Created: now,
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		DataDir:    t.TempDir(),
		Summariser: transcriptEchoSummariser{},
		Store:      db,
		Now:        func() time.Time { return now.Add(time.Hour) },
	}

	if _, err := runner.resummarise(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	note, ok, err := db.GetArtefact(context.Background(), "job1-note")
	if err != nil || !ok {
		t.Fatalf("note not found: %v", err)
	}
	if !strings.Contains(note.Content, "Stored transcript text.") || strings.Contains(note.Content, "old note") {
		t.Fatalf("note was not overwritten from transcript: %s", note.Content)
	}
	updated, ok, err := db.GetJob(context.Background(), "job1")
	if err != nil || !ok {
		t.Fatalf("job not found: %v", err)
	}
	if updated.Message != "Briefing redone" {
		t.Fatalf("unexpected job message: %+v", updated)
	}
}

func TestRunnerRetranscribeOverwritesTranscriptAndNote(t *testing.T) {
	db := store.New(filepath.Join(t.TempDir(), "db.json"))
	if err := db.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	job := store.Job{
		ID: "job1", SourceURL: "https://example.com/video", Title: "Original", Tags: []string{"cyber"},
		Status: "complete", Stage: "complete", NoteID: "job1-note", TranscriptID: "job1-transcript", Created: now, Updated: now,
	}
	if err := db.UpsertJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := db.AddArtefact(context.Background(), store.Artefact{
		ID: "job1-transcript", JobID: "job1", Name: "transcript.md", Kind: "transcript", MediaType: "text/markdown", Content: "old transcript", Created: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddArtefact(context.Background(), store.Artefact{
		ID: "job1-note", JobID: "job1", Name: "old.md", Kind: "note", MediaType: "text/markdown", Content: "old note", Created: now,
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		DataDir:     t.TempDir(),
		Downloader:  fakeDownloader{},
		Transcriber: fakeTranscriber{},
		Summariser:  transcriptEchoSummariser{},
		Store:       db,
		Now:         func() time.Time { return now.Add(time.Hour) },
	}

	if _, err := runner.retranscribe(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	transcriptArtefact, ok, err := db.GetArtefact(context.Background(), "job1-transcript")
	if err != nil || !ok {
		t.Fatalf("transcript not found: %v", err)
	}
	if strings.Contains(transcriptArtefact.Content, "old transcript") || !strings.Contains(transcriptArtefact.Content, "[00:00] Transcript") {
		t.Fatalf("transcript was not overwritten: %s", transcriptArtefact.Content)
	}
	note, ok, err := db.GetArtefact(context.Background(), "job1-note")
	if err != nil || !ok {
		t.Fatalf("note not found: %v", err)
	}
	if strings.Contains(note.Content, "old note") || !strings.Contains(note.Content, "[00:00] Transcript") {
		t.Fatalf("note was not regenerated from transcript: %s", note.Content)
	}
}

func TestTranscriptFromMarkdownStripsFrontmatterAndTitle(t *testing.T) {
	got := transcriptFromMarkdown(notesFixture("Transcript body."))
	if got != "Transcript body." {
		t.Fatalf("got %q", got)
	}
}

type transcriptEchoSummariser struct{}

func (transcriptEchoSummariser) Summarise(ctx context.Context, input summariser.SummaryInput) (summariser.Briefing, error) {
	return summariser.Briefing{
		Title:              "Redone",
		Summary:            input.Transcript,
		KeyPoints:          []string{"Point"},
		PracticalTakeaways: []string{"Takeaway"},
		Questions:          []string{"Question?"},
	}, nil
}

func notesFixture(body string) string {
	return "---\ntype: transcript\n---\n\n# Transcript\n\n" + body
}
