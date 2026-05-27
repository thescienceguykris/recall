package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type DownloadResult struct {
	AudioPath    string
	MetadataPath string
	Title        string
	SourceURL    string
}

type Downloader interface {
	DownloadAudio(ctx context.Context, url string, workDir string) (DownloadResult, error)
}

type YTDLPDownloader struct {
	ExtraArgs []string
}

func (d YTDLPDownloader) DownloadAudio(ctx context.Context, videoURL string, workDir string) (DownloadResult, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return DownloadResult{}, err
	}
	outputTemplate := filepath.Join(workDir, "media.%(ext)s")
	args := []string{
		"--no-playlist",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"--write-info-json",
		"--no-simulate",
		"--output", outputTemplate,
	}
	args = append(args, d.ExtraArgs...)
	args = append(args, videoURL)
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return DownloadResult{}, fmt.Errorf("yt-dlp failed: %w: %s%s", err, strings.TrimSpace(stderr.String()), youtubeHint(stderr.String()))
	}

	audioPath, err := findAudio(workDir)
	if err != nil {
		return DownloadResult{}, err
	}
	metadataPath := filepath.Join(workDir, "media.info.json")
	title := ""
	if data, err := os.ReadFile(metadataPath); err == nil {
		var meta struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(data, &meta) == nil {
			title = meta.Title
		}
	}
	return DownloadResult{AudioPath: audioPath, MetadataPath: metadataPath, Title: title, SourceURL: videoURL}, nil
}

func youtubeHint(stderr string) string {
	lower := strings.ToLower(stderr)
	if !strings.Contains(lower, "[youtube]") {
		return ""
	}
	if strings.Contains(lower, "precondition check failed") || strings.Contains(lower, "http error 403") || strings.Contains(lower, "http error 400") || strings.Contains(lower, "nsig extraction failed") {
		return " (YouTube extraction often requires the latest yt-dlp. Rebuild the image, and if the video still fails, try YTDLP_EXTRA_ARGS such as --cookies=/data/cookies.txt or --extractor-args=youtube:player_client=default,-tv.)"
	}
	return ""
}

func findAudio(workDir string) (string, error) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".mp3" || ext == ".m4a" || ext == ".opus" || ext == ".wav" {
			return filepath.Join(workDir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("yt-dlp completed but no audio file was found in %s", workDir)
}
