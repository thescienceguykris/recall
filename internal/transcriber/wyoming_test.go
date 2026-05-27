package transcriber

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestWyomingWriteAndReadEvent(t *testing.T) {
	var buf bytes.Buffer
	err := writeEvent(&buf, wyomingEvent{
		Type:    "audio-chunk",
		Data:    map[string]any{"rate": 16000, "width": 2, "channels": 1},
		Payload: []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	event, err := readEvent(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "audio-chunk" {
		t.Fatalf("Type = %q", event.Type)
	}
	if event.Data["rate"].(float64) != 16000 {
		t.Fatalf("rate = %#v", event.Data["rate"])
	}
}

func TestWyomingReadTranscript(t *testing.T) {
	input := strings.NewReader(`{"type":"transcript","data":{"text":"hello world"}}` + "\n")
	text, err := readTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello world" {
		t.Fatalf("text = %q", text)
	}
}

func TestWyomingReadTranscriptChunks(t *testing.T) {
	input := strings.NewReader(
		`{"type":"transcript-chunk","data":{"text":"hello "}}` + "\n" +
			`{"type":"transcript-chunk","data":{"text":"world"}}` + "\n" +
			`{"type":"transcript-stop"}` + "\n",
	)
	text, err := readTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello world" {
		t.Fatalf("text = %q", text)
	}
}
