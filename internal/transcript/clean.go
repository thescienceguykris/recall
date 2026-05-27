package transcript

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func Clean(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	input = strings.ToValidUTF8(input, "")

	var b strings.Builder
	b.Grow(len(input))
	lastWasSpace := false
	blankLines := 0
	lineHasText := false

	for _, r := range input {
		if r == utf8.RuneError {
			continue
		}
		switch {
		case r == '\n':
			textLine := lineHasText
			if textLine {
				blankLines = 0
			} else {
				blankLines++
			}
			if textLine || blankLines <= 1 {
				trimTrailingSpace(&b)
				b.WriteByte('\n')
			}
			lastWasSpace = false
			lineHasText = false
		case r == '\t' || unicode.IsSpace(r):
			if lineHasText && !lastWasSpace {
				b.WriteByte(' ')
				lastWasSpace = true
			}
		case unicode.IsControl(r):
			continue
		default:
			b.WriteRune(replacement(r))
			lastWasSpace = false
			lineHasText = true
		}
	}

	return strings.TrimSpace(b.String())
}

func replacement(r rune) rune {
	switch r {
	case '�':
		return -1
	case '‘', '’', '‚', '‛':
		return '\''
	case '“', '”', '„', '‟':
		return '"'
	case '–', '—':
		return '-'
	case '…':
		return '.'
	default:
		return r
	}
}

func trimTrailingSpace(b *strings.Builder) {
	value := b.String()
	trimmed := strings.TrimRight(value, " ")
	if len(trimmed) == len(value) {
		return
	}
	b.Reset()
	b.WriteString(trimmed)
}
