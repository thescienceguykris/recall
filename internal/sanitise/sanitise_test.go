package sanitise

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"basic", "Useful Video Briefing", "Useful-Video-Briefing"},
		{"separators", "../bad/path\\name", "bad-path-name"},
		{"unsafe", "what: now? * yes", "what-now-yes"},
		{"empty", "   ", "fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Filename(tt.in, "fallback"); got != tt.want {
				t.Fatalf("Filename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilenameLimitsLength(t *testing.T) {
	got := Filename(strings.Repeat("a", 200), "fallback")
	if len(got) > MaxFilenameLength {
		t.Fatalf("name too long: %d", len(got))
	}
}

func TestUniquePathAppendsSuffix(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "note.md")
	if err := os.WriteFile(first, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := UniquePath(dir, "note", ".md")
	if got != filepath.Join(dir, "note-2.md") {
		t.Fatalf("UniquePath() = %q", got)
	}
}
