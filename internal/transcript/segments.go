package transcript

import (
	"fmt"
	"strings"
)

type Segment struct {
	Start   float64
	End     float64
	Text    string
	Speaker string
}

type Word struct {
	Start   float64
	End     float64
	Text    string
	Speaker string
}

type Result struct {
	Text     string
	Segments []Segment
	Words    []Word
}

func CleanResult(result Result) Result {
	result.Text = Clean(result.Text)
	cleaned := make([]Segment, 0, len(result.Segments))
	for _, segment := range result.Segments {
		text := Clean(segment.Text)
		if text == "" {
			continue
		}
		segment.Text = text
		segment.Speaker = Clean(segment.Speaker)
		cleaned = append(cleaned, segment)
	}
	result.Segments = cleaned

	words := make([]Word, 0, len(result.Words))
	for _, word := range result.Words {
		text := Clean(word.Text)
		if text == "" {
			continue
		}
		word.Text = text
		word.Speaker = Clean(word.Speaker)
		words = append(words, word)
	}
	result.Words = words

	if result.Text == "" && len(cleaned) > 0 {
		var parts []string
		for _, segment := range cleaned {
			parts = append(parts, segment.Text)
		}
		result.Text = Clean(strings.Join(parts, " "))
	}
	return result
}

func Paragraphs(result Result) string {
	result = CleanResult(result)
	if len(result.Words) > 0 {
		result.Words = applySegmentSpeakers(result.Words, result.Segments)
		return wordParagraphs(result.Words)
	}
	if len(result.Segments) == 0 {
		return result.Text
	}

	const (
		gapSeconds       = 1.5
		maxParagraphSecs = 45.0
		maxParagraphLen  = 900
	)

	var b strings.Builder
	var paragraph []Segment
	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("[")
		b.WriteString(formatTimestamp(paragraph[0].Start))
		b.WriteString("] ")
		if speaker := paragraph[0].Speaker; speaker != "" {
			b.WriteString(speaker)
			b.WriteString(": ")
		}
		for i, segment := range paragraph {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(segment.Text)
		}
		paragraph = nil
	}

	var charCount int
	for _, segment := range result.Segments {
		if len(paragraph) > 0 {
			prev := paragraph[len(paragraph)-1]
			duration := prev.End - paragraph[0].Start
			gap := segment.Start - prev.End
			if segment.Speaker != "" && prev.Speaker != "" && segment.Speaker != prev.Speaker ||
				gap >= gapSeconds || duration >= maxParagraphSecs || charCount+len(segment.Text) > maxParagraphLen {
				flush()
				charCount = 0
			}
		}
		paragraph = append(paragraph, segment)
		charCount += len(segment.Text)
	}
	flush()
	return b.String()
}

func wordParagraphs(words []Word) string {
	const (
		softGapSeconds   = 0.9
		hardGapSeconds   = 1.8
		maxParagraphSecs = 50.0
		maxParagraphLen  = 850
	)

	var b strings.Builder
	var paragraph []Word
	var charCount int

	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("[")
		b.WriteString(formatTimestamp(paragraph[0].Start))
		b.WriteString("] ")
		if speaker := paragraph[0].Speaker; speaker != "" {
			b.WriteString(speaker)
			b.WriteString(": ")
		}
		b.WriteString(joinWords(paragraph))
		paragraph = nil
		charCount = 0
	}

	for _, word := range words {
		if len(paragraph) > 0 {
			prev := paragraph[len(paragraph)-1]
			gap := word.Start - prev.End
			duration := prev.End - paragraph[0].Start
			text := joinWords(paragraph)
			shouldBreak := gap >= hardGapSeconds ||
				(word.Speaker != "" && prev.Speaker != "" && word.Speaker != prev.Speaker) ||
				(gap >= softGapSeconds && endsThought(prev.Text)) ||
				(gap >= softGapSeconds && startsTransition(word.Text)) ||
				duration >= maxParagraphSecs ||
				charCount+len(word.Text) > maxParagraphLen
			if shouldBreak && strings.TrimSpace(text) != "" {
				flush()
			}
		}
		paragraph = append(paragraph, word)
		charCount += len(word.Text) + 1
	}
	flush()
	return b.String()
}

func applySegmentSpeakers(words []Word, segments []Segment) []Word {
	if len(words) == 0 || len(segments) == 0 {
		return words
	}
	for i := range words {
		if words[i].Speaker != "" {
			continue
		}
		for _, segment := range segments {
			if segment.Speaker == "" {
				continue
			}
			if words[i].Start >= segment.Start && words[i].Start <= segment.End {
				words[i].Speaker = segment.Speaker
				break
			}
		}
	}
	return words
}

func joinWords(words []Word) string {
	var b strings.Builder
	for _, word := range words {
		text := strings.TrimSpace(word.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 && !isClosingPunctuation(text) {
			b.WriteByte(' ')
		}
		b.WriteString(text)
	}
	return Clean(b.String())
}

func endsThought(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasSuffix(text, ".") || strings.HasSuffix(text, "?") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, ":")
}

func startsTransition(text string) bool {
	switch strings.ToLower(strings.Trim(text, " \t\n\r\"'“”‘’.,:;!?")) {
	case "so", "but", "however", "next", "then", "now", "also", "finally", "firstly", "secondly", "thirdly", "meanwhile":
		return true
	default:
		return false
	}
}

func isClosingPunctuation(text string) bool {
	return text == "." || text == "," || text == "!" || text == "?" || text == ":" || text == ";" || text == ")" || text == "]"
}

func formatTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds + 0.5)
	hours := total / 3600
	minutes := (total % 3600) / 60
	secs := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%02d:%02d", minutes, secs)
}
