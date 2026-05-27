package sanitise

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const MaxFilenameLength = 96

func Filename(input, fallback string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		input = fallback
	}

	var b strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case r == '/' || r == '\\' || r == 0:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r):
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	name := strings.Trim(b.String(), "-._ ")
	if name == "" {
		name = fallback
	}
	if len(name) > MaxFilenameLength {
		name = strings.Trim(name[:MaxFilenameLength], "-._ ")
	}
	if name == "" {
		name = "untitled"
	}
	return name
}

func UniquePath(dir, base, ext string) string {
	base = Filename(base, "untitled")
	ext = strings.TrimPrefix(ext, ".")
	candidate := filepath.Join(dir, base+"."+ext)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for i := 2; ; i++ {
		candidate = filepath.Join(dir, base+"-"+itoa(i)+"."+ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func itoa(n int) string {
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
