package notes

import (
	"bytes"
	"strings"
	"time"

	"recall/internal/summariser"
)

type NoteInput struct {
	SourceURL      string
	Created        time.Time
	Tags           []string
	TranscriptID   string
	TranscriptName string
	Briefing       summariser.Briefing
}

func RenderNote(input NoteInput) string {
	var b bytes.Buffer
	title := strings.TrimSpace(input.Briefing.Title)
	if title == "" {
		title = "Untitled briefing"
	}

	b.WriteString("---\n")
	writeYAML(&b, "type", "video-briefing")
	writeYAML(&b, "source_url", input.SourceURL)
	writeYAML(&b, "created", input.Created.UTC().Format(time.RFC3339))
	b.WriteString("ai_generated: true\nreviewed: false\n")
	b.WriteString("tags:\n")
	for _, tag := range input.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			b.WriteString("  - ")
			b.WriteString(yamlQuote(tag))
			b.WriteByte('\n')
		}
	}
	writeYAML(&b, "transcript_id", input.TranscriptID)
	b.WriteString("---\n\n")

	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Briefing\n\n")
	b.WriteString(emptyDash(input.Briefing.Summary) + "\n\n")
	writeList(&b, "## Key points", input.Briefing.KeyPoints)
	writeList(&b, "## Practical takeaways", input.Briefing.PracticalTakeaways)
	writeList(&b, "## Questions / follow-ups", input.Briefing.Questions)
	b.WriteString("## Source\n\n")
	b.WriteString(input.SourceURL + "\n\n")
	b.WriteString("## Transcript\n\n")
	if input.TranscriptID != "" {
		b.WriteString("[Open transcript](/artefacts/" + input.TranscriptID + ")\n")
	} else {
		b.WriteString(emptyDash(input.TranscriptName) + "\n")
	}
	return b.String()
}

func RenderTranscript(title, sourceURL, transcript string, created time.Time) string {
	var b bytes.Buffer
	b.WriteString("---\n")
	writeYAML(&b, "type", "video-transcript")
	writeYAML(&b, "source_url", sourceURL)
	writeYAML(&b, "created", created.UTC().Format(time.RFC3339))
	b.WriteString("---\n\n")
	if strings.TrimSpace(title) == "" {
		title = "Transcript"
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString(strings.TrimSpace(transcript))
	b.WriteByte('\n')
	return b.String()
}

func writeList(b *bytes.Buffer, heading string, items []string) {
	b.WriteString(heading + "\n\n")
	if len(items) == 0 {
		b.WriteString("-\n\n")
		return
	}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			b.WriteString("- " + item + "\n")
		}
	}
	b.WriteByte('\n')
}

func writeYAML(b *bytes.Buffer, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(yamlQuote(value))
	b.WriteByte('\n')
}

func yamlQuote(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func emptyDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}
