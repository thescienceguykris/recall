package transcriber

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"

	"recall/internal/transcript"
)

const (
	wyomingRate     = 16000
	wyomingWidth    = 2
	wyomingChannels = 1
)

type WyomingClient struct {
	Addr     string
	Model    string
	Language string
}

func (c WyomingClient) Check(ctx context.Context) error {
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("wyoming whisper address is required")
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return fmt.Errorf("connect to wyoming whisper %s: %w", c.Addr, err)
	}
	return conn.Close()
}

func (c WyomingClient) Transcribe(ctx context.Context, audioPath string) (transcript.Result, error) {
	if strings.TrimSpace(c.Addr) == "" {
		return transcript.Result{}, fmt.Errorf("wyoming whisper address is required")
	}

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return transcript.Result{}, fmt.Errorf("connect to wyoming whisper %s: %w", c.Addr, err)
	}
	defer conn.Close()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if err := writeEvent(conn, wyomingEvent{
		Type: "transcribe",
		Data: cleanData(map[string]any{
			"name":     c.Model,
			"language": c.Language,
		}),
	}); err != nil {
		return transcript.Result{}, fmt.Errorf("send wyoming transcribe event: %w", err)
	}
	if err := writeEvent(conn, wyomingEvent{
		Type: "audio-start",
		Data: map[string]any{"rate": wyomingRate, "width": wyomingWidth, "channels": wyomingChannels},
	}); err != nil {
		return transcript.Result{}, fmt.Errorf("send wyoming audio-start event: %w", err)
	}

	if err := streamPCM(ctx, conn, audioPath); err != nil {
		return transcript.Result{}, err
	}

	if err := writeEvent(conn, wyomingEvent{Type: "audio-stop"}); err != nil {
		return transcript.Result{}, fmt.Errorf("send wyoming audio-stop event: %w", err)
	}

	text, err := readTranscript(conn)
	if err != nil {
		return transcript.Result{}, err
	}
	if strings.TrimSpace(text) == "" {
		return transcript.Result{}, fmt.Errorf("wyoming transcript was empty")
	}
	return transcript.Result{Text: text}, nil
}

func streamPCM(ctx context.Context, w io.Writer, audioPath string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", audioPath,
		"-ac", "1",
		"-ar", "16000",
		"-f", "s16le",
		"pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg for wyoming transcription: %w", err)
	}

	buf := make([]byte, wyomingRate*wyomingWidth/2)
	var offsetBytes int
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			timestamp := offsetBytes * 1000 / (wyomingRate * wyomingWidth * wyomingChannels)
			if err := writeEvent(w, wyomingEvent{
				Type: "audio-chunk",
				Data: map[string]any{
					"rate":      wyomingRate,
					"width":     wyomingWidth,
					"channels":  wyomingChannels,
					"timestamp": timestamp,
				},
				Payload: buf[:n],
			}); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return fmt.Errorf("send wyoming audio-chunk event: %w", err)
			}
			offsetBytes += n
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("read ffmpeg PCM output: %w", readErr)
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg convert audio for wyoming transcription: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func readTranscript(r io.Reader) (string, error) {
	reader := bufio.NewReader(r)
	var chunks []string
	for {
		event, err := readEvent(reader)
		if err != nil {
			return "", fmt.Errorf("read wyoming transcript event: %w", err)
		}
		switch event.Type {
		case "transcript":
			if text, _ := event.Data["text"].(string); strings.TrimSpace(text) != "" {
				return text, nil
			}
		case "transcript-chunk":
			if text, _ := event.Data["text"].(string); text != "" {
				chunks = append(chunks, text)
			}
		case "transcript-stop":
			if len(chunks) > 0 {
				return strings.Join(chunks, ""), nil
			}
		case "error":
			return "", fmt.Errorf("wyoming server error: %v", event.Data)
		}
	}
}

type wyomingEvent struct {
	Type    string
	Data    map[string]any
	Payload []byte
}

func writeEvent(w io.Writer, event wyomingEvent) error {
	header := map[string]any{"type": event.Type}
	if len(event.Data) > 0 {
		header["data"] = event.Data
	}
	if len(event.Payload) > 0 {
		header["payload_length"] = len(event.Payload)
	}
	raw, err := json.Marshal(header)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if len(event.Payload) > 0 {
		_, err = w.Write(event.Payload)
	}
	return err
}

func readEvent(r *bufio.Reader) (wyomingEvent, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return wyomingEvent{}, err
	}
	var header struct {
		Type          string         `json:"type"`
		Data          map[string]any `json:"data"`
		DataLength    int            `json:"data_length"`
		PayloadLength int            `json:"payload_length"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(line), &header); err != nil {
		return wyomingEvent{}, err
	}
	if header.Type == "" {
		return wyomingEvent{}, fmt.Errorf("wyoming event missing type")
	}
	if header.Data == nil {
		header.Data = map[string]any{}
	}
	if header.DataLength > 0 {
		data := make([]byte, header.DataLength)
		if _, err := io.ReadFull(r, data); err != nil {
			return wyomingEvent{}, err
		}
		var extra map[string]any
		if err := json.Unmarshal(data, &extra); err != nil {
			return wyomingEvent{}, err
		}
		for key, value := range extra {
			header.Data[key] = value
		}
	}
	if header.PayloadLength > 0 {
		if _, err := io.CopyN(io.Discard, r, int64(header.PayloadLength)); err != nil {
			return wyomingEvent{}, err
		}
	}
	return wyomingEvent{Type: header.Type, Data: header.Data}, nil
}

func cleanData(data map[string]any) map[string]any {
	cleaned := map[string]any{}
	for key, value := range data {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				cleaned[key] = v
			}
		default:
			cleaned[key] = value
		}
	}
	return cleaned
}
