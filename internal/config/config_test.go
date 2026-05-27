package config

import (
	"testing"
	"time"
)

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("WHISPER_BASE_URL", "http://whisper.local/")
	t.Setenv("LLM_BASE_URL", "http://llm.local/")
	t.Setenv("LLM_MODEL", "local-model")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.DataDir != "/data" {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.DBPath != "/data/recall-db.json" {
		t.Fatalf("DBPath = %q", cfg.DBPath)
	}
	if cfg.StoreType != "json" || cfg.AuthMode != "none" {
		t.Fatalf("unexpected store/auth defaults: %+v", cfg)
	}
	if cfg.JobTimeout != time.Hour {
		t.Fatalf("JobTimeout = %s", cfg.JobTimeout)
	}
	if cfg.WhisperBaseURL != "http://whisper.local" {
		t.Fatalf("WhisperBaseURL = %q", cfg.WhisperBaseURL)
	}
}

func TestLoadFromEnvOIDCRequiresPostgres(t *testing.T) {
	t.Setenv("WHISPER_BASE_URL", "http://whisper")
	t.Setenv("LLM_BASE_URL", "http://llm")
	t.Setenv("LLM_MODEL", "model")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://issuer.example")
	t.Setenv("OIDC_CLIENT_ID", "client")
	t.Setenv("OIDC_CLIENT_SECRET", "secret")
	t.Setenv("OIDC_REDIRECT_URL", "https://recall.example/auth/callback")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnvPostgresOIDC(t *testing.T) {
	t.Setenv("WHISPER_BASE_URL", "http://whisper")
	t.Setenv("LLM_BASE_URL", "http://llm")
	t.Setenv("LLM_MODEL", "model")
	t.Setenv("STORE_TYPE", "postgres")
	t.Setenv("DATABASE_URL", "postgres://recall:recall@postgres:5432/recall?sslmode=disable")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://issuer.example/")
	t.Setenv("OIDC_CLIENT_ID", "client")
	t.Setenv("OIDC_CLIENT_SECRET", "secret")
	t.Setenv("OIDC_REDIRECT_URL", "https://recall.example/auth/callback")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	t.Setenv("AUTH_ALLOWED_DOMAINS", "example.com, example.org")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StoreType != "postgres" || cfg.AuthMode != "oidc" || cfg.OIDCIssuerURL != "https://issuer.example" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if len(cfg.AllowedDomains) != 2 {
		t.Fatalf("AllowedDomains = %#v", cfg.AllowedDomains)
	}
}

func TestLoadFromEnvMissingRequired(t *testing.T) {
	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnvOverrides(t *testing.T) {
	t.Setenv("ADDR", ":9090")
	t.Setenv("DATA_DIR", "/tmp/data")
	t.Setenv("DB_PATH", "/tmp/recall.json")
	t.Setenv("WHISPER_BASE_URL", "http://whisper")
	t.Setenv("LLM_BASE_URL", "http://llm")
	t.Setenv("LLM_MODEL", "model")
	t.Setenv("JOB_TIMEOUT_SECONDS", "12")
	t.Setenv("YTDLP_EXTRA_ARGS", "--extractor-args=youtube:player_client=default,-tv --cookies=/data/cookies.txt")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9090" || cfg.DataDir != "/tmp/data" || cfg.DBPath != "/tmp/recall.json" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.JobTimeout != 12*time.Second {
		t.Fatalf("JobTimeout = %s", cfg.JobTimeout)
	}
	if len(cfg.YTDLPExtraArgs) != 2 {
		t.Fatalf("YTDLPExtraArgs = %#v", cfg.YTDLPExtraArgs)
	}
}

func TestLoadFromEnvWyomingTranscriber(t *testing.T) {
	t.Setenv("TRANSCRIBER_TYPE", "wyoming")
	t.Setenv("WYOMING_WHISPER_ADDR", "faster-whisper:10300")
	t.Setenv("WYOMING_WHISPER_LANGUAGE", "en")
	t.Setenv("LLM_BASE_URL", "http://llm")
	t.Setenv("LLM_MODEL", "model")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TranscriberType != "wyoming" {
		t.Fatalf("TranscriberType = %q", cfg.TranscriberType)
	}
	if cfg.WyomingAddr != "faster-whisper:10300" || cfg.WyomingLanguage != "en" {
		t.Fatalf("unexpected wyoming config: %+v", cfg)
	}
}

func TestLoadFromEnvWyomingRequiresAddr(t *testing.T) {
	t.Setenv("TRANSCRIBER_TYPE", "wyoming")
	t.Setenv("LLM_BASE_URL", "http://llm")
	t.Setenv("LLM_MODEL", "model")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error")
	}
}
