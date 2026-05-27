package summariser

import (
	"strings"
	"testing"
)

func TestParseBriefingJSON(t *testing.T) {
	raw := `{"title":"Briefing","summary":"Summary","key_points":["A"],"practical_takeaways":["B"],"questions":["C"],"suggested_tags":["D"]}`
	briefing, err := ParseBriefingJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if briefing.Title != "Briefing" || len(briefing.KeyPoints) != 1 {
		t.Fatalf("unexpected briefing: %+v", briefing)
	}
}

func TestParseBriefingJSONRejectsInvalid(t *testing.T) {
	if _, err := ParseBriefingJSON(`not json`); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseBriefingJSONRejectsMissingTitle(t *testing.T) {
	if _, err := ParseBriefingJSON(`{"summary":"x"}`); err == nil {
		t.Fatal("expected error")
	}
}

func TestChunkTranscriptSplitsOnParagraphs(t *testing.T) {
	transcript := strings.Join([]string{
		strings.Repeat("a", 20),
		strings.Repeat("b", 20),
		strings.Repeat("c", 20),
	}, "\n\n")

	chunks := chunkTranscript(transcript, 45)
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, chunks=%#v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0], strings.Repeat("a", 20)) || !strings.Contains(chunks[0], strings.Repeat("b", 20)) {
		t.Fatalf("unexpected first chunk: %q", chunks[0])
	}
}

func TestShouldChunkTranscriptUsesBenefitThreshold(t *testing.T) {
	input := strings.Repeat("a", chunkBenefitThresholdChars)
	if shouldChunkTranscript(input) {
		t.Fatal("should not chunk at threshold")
	}
	input += "a"
	if !shouldChunkTranscript(input) {
		t.Fatal("should chunk above threshold")
	}
}

func TestSystemPromptDoesNotOverConstrainLength(t *testing.T) {
	prompt := systemPrompt("final")
	if strings.Contains(strings.ToLower(prompt), "concise") {
		t.Fatalf("prompt should not request concise output: %s", prompt)
	}
	for _, want := range []string{"Do not compress important detail", "12-20", "multi-paragraph", "British English"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}
