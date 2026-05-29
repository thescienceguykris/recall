package transcriber

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWhisperCheckAcceptsAnyHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	client := WhisperClient{BaseURL: server.URL, HTTPClient: server.Client()}
	if err := client.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}
