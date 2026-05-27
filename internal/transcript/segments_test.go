package transcript

import (
	"strings"
	"testing"
)

func TestParagraphsUsesTimedSegments(t *testing.T) {
	got := Paragraphs(Result{
		Segments: []Segment{
			{Start: 0.1, End: 1.0, Text: " hello  there "},
			{Start: 1.1, End: 2.0, Text: "general kenobi"},
			{Start: 5.0, End: 6.0, Text: "new paragraph"},
		},
	})
	want := "[00:00] hello there general kenobi\n\n[00:05] new paragraph"
	if got != want {
		t.Fatalf("Paragraphs() = %q, want %q", got, want)
	}
}

func TestParagraphsPrefersWordTimings(t *testing.T) {
	got := Paragraphs(Result{
		Segments: []Segment{{Start: 0, End: 10, Text: "segment text should not be used"}},
		Words: []Word{
			{Start: 0, End: 0.2, Text: "This"},
			{Start: 0.2, End: 0.4, Text: "is"},
			{Start: 0.4, End: 0.6, Text: "one"},
			{Start: 0.6, End: 0.8, Text: "thought."},
			{Start: 2.0, End: 2.2, Text: "However"},
			{Start: 2.2, End: 2.4, Text: "this"},
			{Start: 2.4, End: 2.6, Text: "moves"},
			{Start: 2.6, End: 2.8, Text: "on."},
		},
	})
	want := "[00:00] This is one thought.\n\n[00:02] However this moves on."
	if got != want {
		t.Fatalf("Paragraphs() = %q, want %q", got, want)
	}
}

func TestParagraphsIncludesSegmentSpeakers(t *testing.T) {
	got := Paragraphs(Result{
		Segments: []Segment{
			{Start: 0, End: 1, Text: "hello there", Speaker: "SPEAKER_00"},
			{Start: 1.2, End: 2, Text: "general kenobi", Speaker: "SPEAKER_01"},
		},
	})
	want := "[00:00] SPEAKER_00: hello there\n\n[00:01] SPEAKER_01: general kenobi"
	if got != want {
		t.Fatalf("Paragraphs() = %q, want %q", got, want)
	}
}

func TestParagraphsMapsSegmentSpeakersToWords(t *testing.T) {
	got := Paragraphs(Result{
		Segments: []Segment{
			{Start: 0, End: 1, Text: "hello there", Speaker: "SPEAKER_00"},
			{Start: 2, End: 3, Text: "reply here", Speaker: "SPEAKER_01"},
		},
		Words: []Word{
			{Start: 0, End: 0.2, Text: "hello"},
			{Start: 0.3, End: 0.5, Text: "there"},
			{Start: 2, End: 2.2, Text: "reply"},
			{Start: 2.3, End: 2.5, Text: "here"},
		},
	})
	want := "[00:00] SPEAKER_00: hello there\n\n[00:02] SPEAKER_01: reply here"
	if got != want {
		t.Fatalf("Paragraphs() = %q, want %q", got, want)
	}
}

func TestParagraphsFallsBackToPlainText(t *testing.T) {
	got := Paragraphs(Result{Text: "hello   world"})
	if got != "hello world" {
		t.Fatalf("Paragraphs() = %q", got)
	}
}

func TestParagraphsFormatsHourTimestamp(t *testing.T) {
	got := Paragraphs(Result{Segments: []Segment{{Start: 3661, End: 3662, Text: "late segment"}}})
	if !strings.HasPrefix(got, "[1:01:01]") {
		t.Fatalf("Paragraphs() = %q", got)
	}
}
