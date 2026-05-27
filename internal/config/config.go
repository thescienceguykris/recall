package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr             string
	DataDir          string
	DBPath           string
	StoreType        string
	DatabaseURL      string
	TranscriberType  string
	WhisperBaseURL   string
	WyomingAddr      string
	WyomingModel     string
	WyomingLanguage  string
	LLMBaseURL       string
	LLMAPIKey        string
	LLMModel         string
	JobTimeout       time.Duration
	NoteOutputDir    string
	TranscriptOutDir string
	YTDLPExtraArgs   []string
	AuthMode         string
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	SessionSecret    string
	AllowedEmails    []string
	AllowedDomains   []string
}

func LoadFromEnv() (Config, error) {
	dataDir := getenv("DATA_DIR", "/data")
	cfg := Config{
		Addr:             getenv("ADDR", ":8080"),
		DataDir:          dataDir,
		DBPath:           getenv("DB_PATH", dataDir+"/recall-db.json"),
		StoreType:        strings.ToLower(getenv("STORE_TYPE", "json")),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		TranscriberType:  strings.ToLower(getenv("TRANSCRIBER_TYPE", "openai")),
		WhisperBaseURL:   strings.TrimRight(os.Getenv("WHISPER_BASE_URL"), "/"),
		WyomingAddr:      os.Getenv("WYOMING_WHISPER_ADDR"),
		WyomingModel:     os.Getenv("WYOMING_WHISPER_MODEL"),
		WyomingLanguage:  os.Getenv("WYOMING_WHISPER_LANGUAGE"),
		LLMBaseURL:       strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/"),
		LLMAPIKey:        os.Getenv("LLM_API_KEY"),
		LLMModel:         os.Getenv("LLM_MODEL"),
		YTDLPExtraArgs:   strings.Fields(os.Getenv("YTDLP_EXTRA_ARGS")),
		AuthMode:         strings.ToLower(getenv("AUTH_MODE", "none")),
		OIDCIssuerURL:    strings.TrimRight(os.Getenv("OIDC_ISSUER_URL"), "/"),
		OIDCClientID:     os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
		SessionSecret:    os.Getenv("SESSION_SECRET"),
		AllowedEmails:    splitCSV(os.Getenv("AUTH_ALLOWED_EMAILS")),
		AllowedDomains:   splitCSV(os.Getenv("AUTH_ALLOWED_DOMAINS")),
	}

	if noteDir := os.Getenv("NOTE_OUTPUT_DIR"); noteDir != "" {
		cfg.NoteOutputDir = noteDir
	}
	if transcriptDir := os.Getenv("TRANSCRIPT_OUTPUT_DIR"); transcriptDir != "" {
		cfg.TranscriptOutDir = transcriptDir
	} else if cfg.NoteOutputDir != "" {
		cfg.TranscriptOutDir = cfg.NoteOutputDir + "/Transcripts"
	}

	seconds, err := strconv.Atoi(getenv("JOB_TIMEOUT_SECONDS", "3600"))
	if err != nil || seconds <= 0 {
		return Config{}, fmt.Errorf("JOB_TIMEOUT_SECONDS must be a positive integer")
	}
	cfg.JobTimeout = time.Duration(seconds) * time.Second

	var missing []string
	switch cfg.TranscriberType {
	case "openai", "whisper":
		cfg.TranscriberType = "openai"
		if cfg.WhisperBaseURL == "" {
			missing = append(missing, "WHISPER_BASE_URL")
		}
	case "wyoming":
		if cfg.WyomingAddr == "" {
			missing = append(missing, "WYOMING_WHISPER_ADDR")
		}
	default:
		return Config{}, fmt.Errorf("TRANSCRIBER_TYPE must be openai or wyoming")
	}
	if cfg.LLMBaseURL == "" {
		missing = append(missing, "LLM_BASE_URL")
	}
	if cfg.LLMModel == "" {
		missing = append(missing, "LLM_MODEL")
	}
	if len(missing) > 0 {
		return Config{}, errors.New("missing required config: " + strings.Join(missing, ", "))
	}
	switch cfg.StoreType {
	case "json":
	case "postgres":
		if cfg.DatabaseURL == "" {
			return Config{}, errors.New("missing required config: DATABASE_URL")
		}
	default:
		return Config{}, fmt.Errorf("STORE_TYPE must be json or postgres")
	}
	switch cfg.AuthMode {
	case "none":
	case "oidc":
		if cfg.StoreType != "postgres" {
			return Config{}, errors.New("AUTH_MODE=oidc requires STORE_TYPE=postgres")
		}
		var authMissing []string
		if cfg.OIDCIssuerURL == "" {
			authMissing = append(authMissing, "OIDC_ISSUER_URL")
		}
		if cfg.OIDCClientID == "" {
			authMissing = append(authMissing, "OIDC_CLIENT_ID")
		}
		if cfg.OIDCClientSecret == "" {
			authMissing = append(authMissing, "OIDC_CLIENT_SECRET")
		}
		if cfg.OIDCRedirectURL == "" {
			authMissing = append(authMissing, "OIDC_REDIRECT_URL")
		}
		if cfg.SessionSecret == "" {
			authMissing = append(authMissing, "SESSION_SECRET")
		}
		if len(authMissing) > 0 {
			return Config{}, errors.New("missing required auth config: " + strings.Join(authMissing, ", "))
		}
	default:
		return Config{}, fmt.Errorf("AUTH_MODE must be none or oidc")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
