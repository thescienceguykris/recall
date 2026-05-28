package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"recall/internal/auth"
	"recall/internal/config"
	"recall/internal/downloader"
	"recall/internal/httpapi"
	"recall/internal/jobs"
	"recall/internal/store"
	"recall/internal/summariser"
	"recall/internal/transcriber"
)

type appStore interface {
	Init(context.Context) error
	EnsureUser(context.Context, store.User) (store.User, error)
	UpsertJob(context.Context, store.Job) error
	AddArtefact(context.Context, store.Artefact) error
	UpsertArtefact(context.Context, store.Artefact) error
	GetJob(context.Context, string) (store.Job, bool, error)
	GetArtefact(context.Context, string) (store.Artefact, bool, error)
	ListJobs(context.Context) ([]store.Job, error)
	DeleteJob(context.Context, string) (bool, error)
}

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	db, err := buildStore(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := initStore(context.Background(), cfg, db); err != nil {
		log.Fatal(err)
	}
	if _, err := db.EnsureUser(context.Background(), store.User{ID: store.LocalUserID, Provider: "local", ProviderSubject: store.LocalUserID, Name: "Local"}); err != nil {
		log.Fatal(err)
	}
	authSvc, err := buildAuth(context.Background(), cfg, db)
	if err != nil {
		log.Fatal(err)
	}

	client := &http.Client{Timeout: cfg.JobTimeout}
	transcriberClient := buildTranscriber(cfg, client)
	runner := jobs.Runner{
		DataDir:     cfg.DataDir,
		Downloader:  downloader.YTDLPDownloader{ExtraArgs: cfg.YTDLPExtraArgs},
		Transcriber: transcriberClient,
		Summariser: summariser.Client{
			BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, HTTPClient: client,
		},
		Store: db,
	}

	server := httpapi.Server{Runner: runner, Store: db, Auth: authSvc, JobTimeout: cfg.JobTimeout}
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("recall listening on %s", cfg.Addr)
	log.Fatal(httpServer.ListenAndServe())
}

func initStore(ctx context.Context, cfg config.Config, db appStore) error {
	const retryDelay = 2 * time.Second
	for {
		err := db.Init(ctx)
		if err == nil {
			return nil
		}
		if cfg.StoreType != "postgres" {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		log.Printf("postgres is not ready; retrying in %s: %v", retryDelay, err)
		time.Sleep(retryDelay)
	}
}

func buildStore(cfg config.Config) (appStore, error) {
	if cfg.StoreType == "postgres" {
		return store.NewPostgres(cfg.DatabaseURL)
	}
	return store.New(cfg.DBPath), nil
}

func buildAuth(ctx context.Context, cfg config.Config, userStore auth.UserStore) (auth.Service, error) {
	if cfg.AuthMode == "oidc" {
		return auth.NewOIDC(ctx, auth.Config{
			Mode: cfg.AuthMode, IssuerURL: cfg.OIDCIssuerURL, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret,
			RedirectURL: cfg.OIDCRedirectURL, SessionSecret: cfg.SessionSecret, AllowedEmails: cfg.AllowedEmails, AllowedDomains: cfg.AllowedDomains,
		}, userStore)
	}
	return auth.Noop{}, nil
}

func buildTranscriber(cfg config.Config, client *http.Client) transcriber.Transcriber {
	if cfg.TranscriberType == "wyoming" {
		return transcriber.WyomingClient{
			Addr:     cfg.WyomingAddr,
			Model:    cfg.WyomingModel,
			Language: cfg.WyomingLanguage,
		}
	}
	return transcriber.WhisperClient{BaseURL: cfg.WhisperBaseURL, HTTPClient: client}
}
