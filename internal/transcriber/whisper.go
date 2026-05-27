package transcriber

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"recall/internal/transcript"
)

type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string) (transcript.Result, error)
}

type WhisperClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c WhisperClient) Transcribe(ctx context.Context, audioPath string) (transcript.Result, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return transcript.Result{}, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return transcript.Result{}, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return transcript.Result{}, err
	}
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return transcript.Result{}, err
	}
	if err := writer.WriteField("response_format", "verbose_json"); err != nil {
		return transcript.Result{}, err
	}
	if err := writer.WriteField("timestamp_granularities[]", "segment"); err != nil {
		return transcript.Result{}, err
	}
	if err := writer.WriteField("timestamp_granularities[]", "word"); err != nil {
		return transcript.Result{}, err
	}
	if err := writer.Close(); err != nil {
		return transcript.Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/audio/transcriptions", &body)
	if err != nil {
		return transcript.Result{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return transcript.Result{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return transcript.Result{}, fmt.Errorf("whisper request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed struct {
		Text     string `json:"text"`
		Segments []struct {
			Start   float64 `json:"start"`
			End     float64 `json:"end"`
			Text    string  `json:"text"`
			Speaker string  `json:"speaker"`
		} `json:"segments"`
		Words []struct {
			Start   float64 `json:"start"`
			End     float64 `json:"end"`
			Word    string  `json:"word"`
			Text    string  `json:"text"`
			Speaker string  `json:"speaker"`
		} `json:"words"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return transcript.Result{}, fmt.Errorf("parse whisper response: %w", err)
	}
	if strings.TrimSpace(parsed.Text) == "" {
		return transcript.Result{}, fmt.Errorf("whisper response did not include transcript text")
	}
	result := transcript.Result{Text: parsed.Text}
	for _, segment := range parsed.Segments {
		result.Segments = append(result.Segments, transcript.Segment{
			Start:   segment.Start,
			End:     segment.End,
			Text:    segment.Text,
			Speaker: segment.Speaker,
		})
	}
	for _, word := range parsed.Words {
		text := word.Word
		if text == "" {
			text = word.Text
		}
		result.Words = append(result.Words, transcript.Word{
			Start:   word.Start,
			End:     word.End,
			Text:    text,
			Speaker: word.Speaker,
		})
	}
	return result, nil
}
