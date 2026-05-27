package notes

import (
	"strings"
	"testing"
	"time"

	"recall/internal/summariser"
)

func TestRenderNote(t *testing.T) {
	md := RenderNote(NoteInput{
		SourceURL:    "https://example.com/video",
		Created:      time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
		Tags:         []string{"cyber", "kubernetes"},
		TranscriptID: "job-transcript",
		Briefing: summariser.Briefing{
			Title:              "Useful briefing",
			Summary:            "A concise summary.",
			KeyPoints:          []string{"Point one"},
			PracticalTakeaways: []string{"Do the thing"},
			Questions:          []string{"What next?"},
		},
	})
	for _, want := range []string{
		"type: \"video-briefing\"",
		"source_url: \"https://example.com/video\"",
		"ai_generated: true",
		"# Useful briefing",
		"## Practical takeaways",
		"[Open transcript](/artefacts/job-transcript)",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("rendered note missing %q:\n%s", want, md)
		}
	}
}

func TestRenderTranscript(t *testing.T) {
	md := RenderTranscript("A transcript", "https://example.com", "hello", time.Now())
	if !strings.Contains(md, "# A transcript") || !strings.Contains(md, "hello\n") {
		t.Fatalf("unexpected transcript:\n%s", md)
	}
}
