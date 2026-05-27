package summariser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	chunkBenefitThresholdChars = 120000
	maxTranscriptCharsPerCall  = 60000
)

type Briefing struct {
	Title              string   `json:"title"`
	Summary            string   `json:"summary"`
	KeyPoints          []string `json:"key_points"`
	PracticalTakeaways []string `json:"practical_takeaways"`
	Questions          []string `json:"questions"`
	SuggestedTags      []string `json:"suggested_tags"`
}

type SummaryInput struct {
	Title      string
	SourceURL  string
	Transcript string
	Tags       []string
	WorkDir    string
}

type Summariser interface {
	Summarise(ctx context.Context, input SummaryInput) (Briefing, error)
}

type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func (c Client) Summarise(ctx context.Context, input SummaryInput) (Briefing, error) {
	if !shouldChunkTranscript(input.Transcript) {
		return c.summariseSingle(ctx, input, "final", input.Transcript)
	}
	chunks := chunkTranscript(input.Transcript, maxTranscriptCharsPerCall)
	if len(chunks) > 1 {
		return c.summariseChunks(ctx, input, chunks)
	}
	return c.summariseSingle(ctx, input, "final", input.Transcript)
}

func shouldChunkTranscript(transcript string) bool {
	return len(strings.TrimSpace(transcript)) > chunkBenefitThresholdChars
}

func (c Client) summariseChunks(ctx context.Context, input SummaryInput, chunks []string) (Briefing, error) {
	briefings := make([]Briefing, 0, len(chunks))
	for i, chunk := range chunks {
		chunkInput := input
		chunkInput.Transcript = chunk
		briefing, err := c.summariseSingle(ctx, chunkInput, fmt.Sprintf("chunk %d of %d", i+1, len(chunks)), chunk)
		if err != nil {
			return Briefing{}, err
		}
		briefings = append(briefings, briefing)
	}

	mergedInput := input
	mergedInput.Transcript = mergePrompt(briefings)
	return c.summariseSingle(ctx, mergedInput, "final merge", mergedInput.Transcript)
}

func (c Client) summariseSingle(ctx context.Context, input SummaryInput, mode, transcript string) (Briefing, error) {
	body := map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt(mode)},
			{"role": "user", "content": buildPrompt(input, transcript)},
		},
		"temperature": 0.2,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Briefing{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Briefing{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Briefing{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Briefing{}, fmt.Errorf("llm request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	content, err := chatContent(respBody)
	if err != nil {
		saveRaw(input.WorkDir, "llm-raw-response-"+safeMode(mode)+".txt", string(respBody))
		return Briefing{}, err
	}
	briefing, err := ParseBriefingJSON(content)
	if err != nil {
		saveRaw(input.WorkDir, "llm-raw-response-"+safeMode(mode)+".txt", content)
		return Briefing{}, err
	}
	return briefing, nil
}

func ParseBriefingJSON(raw string) (Briefing, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var briefing Briefing
	if err := json.Unmarshal([]byte(raw), &briefing); err != nil {
		return Briefing{}, fmt.Errorf("parse llm briefing JSON: %w", err)
	}
	if strings.TrimSpace(briefing.Title) == "" {
		return Briefing{}, fmt.Errorf("parse llm briefing JSON: missing title")
	}
	return briefing, nil
}

func chatContent(body []byte) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse llm response envelope: %w", err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("parse llm response envelope: missing message content")
	}
	return parsed.Choices[0].Message.Content, nil
}

func systemPrompt(mode string) string {
	return "Return JSON only matching this shape: {\"title\":\"\",\"summary\":\"\",\"key_points\":[],\"practical_takeaways\":[],\"questions\":[],\"suggested_tags\":[]}." +
		" Create a detailed but readable briefing note from the transcript or chunk briefings." +
		" Do not compress important detail just to be brief." +
		" Preserve named entities, dates, numbers, claims, caveats, examples, decisions, warnings, and action items." +
		" Do not invent facts not supported by the source text. Preserve uncertainty." +
		" Make the summary multi-paragraph when the source contains multiple topics." +
		" Use enough key points to cover the material; for long material this may be 12-20 items." +
		" Extract practical takeaways and follow-up questions." +
		" Keep the title useful for a note. Prefer British English spelling." +
		" Current pass: " + mode + "."
}

func buildPrompt(input SummaryInput, transcript string) string {
	return fmt.Sprintf("Title: %s\nSource URL: %s\nUser tags: %s\n\nSource text:\n%s", input.Title, input.SourceURL, strings.Join(input.Tags, ", "), transcript)
}

func mergePrompt(briefings []Briefing) string {
	var b strings.Builder
	b.WriteString("Merge these chunk briefings into one complete briefing. Keep detail; remove only true duplication.\n\n")
	for i, briefing := range briefings {
		b.WriteString(fmt.Sprintf("## Chunk %d\n", i+1))
		b.WriteString("Title: " + briefing.Title + "\n")
		b.WriteString("Summary:\n" + briefing.Summary + "\n")
		writePromptList(&b, "Key points", briefing.KeyPoints)
		writePromptList(&b, "Practical takeaways", briefing.PracticalTakeaways)
		writePromptList(&b, "Questions", briefing.Questions)
		writePromptList(&b, "Suggested tags", briefing.SuggestedTags)
		b.WriteByte('\n')
	}
	return b.String()
}

func writePromptList(b *strings.Builder, title string, items []string) {
	b.WriteString(title + ":\n")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			b.WriteString("- " + item + "\n")
		}
	}
}

func chunkTranscript(transcript string, maxChars int) []string {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" || len(transcript) <= maxChars {
		if transcript == "" {
			return nil
		}
		return []string{transcript}
	}

	paragraphs := strings.Split(transcript, "\n\n")
	var chunks []string
	var current strings.Builder
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		if current.Len() > 0 && current.Len()+len(paragraph)+2 > maxChars {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if len(paragraph) > maxChars {
			if current.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(current.String()))
				current.Reset()
			}
			for len(paragraph) > maxChars {
				cut := strings.LastIndex(paragraph[:maxChars], " ")
				if cut < maxChars/2 {
					cut = maxChars
				}
				chunks = append(chunks, strings.TrimSpace(paragraph[:cut]))
				paragraph = strings.TrimSpace(paragraph[cut:])
			}
		}
		if paragraph != "" {
			if current.Len() > 0 {
				current.WriteString("\n\n")
			}
			current.WriteString(paragraph)
		}
	}
	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}
	return chunks
}

func safeMode(mode string) string {
	mode = strings.ReplaceAll(mode, " ", "-")
	mode = strings.ReplaceAll(mode, "/", "-")
	return mode
}

func saveRaw(workDir, name, raw string) {
	if workDir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(workDir, name), []byte(raw), 0o600)
}
