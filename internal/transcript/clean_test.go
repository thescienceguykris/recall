package transcript

import "testing"

func TestCleanCollapsesWhitespace(t *testing.T) {
	input := " hello   world\t\tagain \n\n\n next   line "
	want := "hello world again\n\nnext line"
	if got := Clean(input); got != want {
		t.Fatalf("Clean() = %q, want %q", got, want)
	}
}

func TestCleanNormalisesCharacters(t *testing.T) {
	input := "Here\u0000 are “quotes”, an em—dash, and invalid \xef\xbf\xbd text"
	want := `Here are "quotes", an em-dash, and invalid text`
	if got := Clean(input); got != want {
		t.Fatalf("Clean() = %q, want %q", got, want)
	}
}

func TestCleanKeepsMeaningfulLineBreaks(t *testing.T) {
	input := "first line  \nsecond line"
	want := "first line\nsecond line"
	if got := Clean(input); got != want {
		t.Fatalf("Clean() = %q, want %q", got, want)
	}
}
