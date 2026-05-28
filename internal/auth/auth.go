package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"recall/internal/store"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "recall_session"
	stateCookie   = "recall_oauth_state"
	pkceCookie    = "recall_oauth_pkce"
	sessionMaxAge = 7 * 24 * time.Hour
)

type UserStore interface {
	EnsureUser(ctx context.Context, user store.User) (store.User, error)
}

type Config struct {
	Mode           string
	IssuerURL      string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	SessionSecret  string
	AllowedEmails  []string
	AllowedDomains []string
}

type Service interface {
	Enabled() bool
	CurrentUser(*http.Request) (store.User, bool)
	Login(http.ResponseWriter, *http.Request)
	Callback(http.ResponseWriter, *http.Request)
	Logout(http.ResponseWriter, *http.Request)
}

type Noop struct{}

func (Noop) Enabled() bool { return false }

func (Noop) CurrentUser(r *http.Request) (store.User, bool) {
	return store.User{ID: store.LocalUserID, Provider: "local", ProviderSubject: store.LocalUserID, Name: "Local"}, true
}

func (Noop) Login(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

func (Noop) Callback(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

func (Noop) Logout(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

type OIDC struct {
	store          UserStore
	oauth          oauth2.Config
	verifier       *oidc.IDTokenVerifier
	sessionSecret  []byte
	allowedEmails  map[string]bool
	allowedDomains map[string]bool
	secureCookies  bool
}

func NewOIDC(ctx context.Context, cfg Config, userStore UserStore) (*OIDC, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}
	redirectURL, err := url.Parse(cfg.RedirectURL)
	if err != nil {
		return nil, err
	}
	svc := &OIDC{
		store: userStore,
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier:       provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		sessionSecret:  []byte(cfg.SessionSecret),
		allowedEmails:  setFromStrings(cfg.AllowedEmails),
		allowedDomains: setFromStrings(cfg.AllowedDomains),
		secureCookies:  redirectURL.Scheme == "https",
	}
	if len(svc.sessionSecret) < 32 {
		return nil, errors.New("SESSION_SECRET must be at least 32 characters")
	}
	return svc, nil
}

func (o *OIDC) Enabled() bool { return true }

func (o *OIDC) CurrentUser(r *http.Request) (store.User, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, false
	}
	user, err := o.decodeSession(cookie.Value)
	if err != nil {
		return store.User{}, false
	}
	return user, true
}

func (o *OIDC) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   o.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     pkceCookie,
		Value:    verifier,
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   o.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, o.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (o *OIDC) Callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	oauthStateCookie, err := r.Cookie(stateCookie)
	if err != nil || state == "" || !hmac.Equal([]byte(state), []byte(oauthStateCookie.Value)) {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	pkceVerifierCookie, err := r.Cookie(pkceCookie)
	if err != nil || pkceVerifierCookie.Value == "" {
		http.Error(w, "missing oauth verifier", http.StatusBadRequest)
		return
	}
	token, err := o.oauth.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(pkceVerifierCookie.Value))
	if err != nil {
		http.Error(w, "oauth exchange failed", http.StatusBadGateway)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id token", http.StatusBadGateway)
		return
	}
	idToken, err := o.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "invalid id token", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "invalid id token claims", http.StatusUnauthorized)
		return
	}
	if claims.Email != "" && !claims.EmailVerified {
		http.Error(w, "email is not verified", http.StatusForbidden)
		return
	}
	if !o.allowed(claims.Email) {
		http.Error(w, "user is not allowed", http.StatusForbidden)
		return
	}
	user, err := o.store.EnsureUser(r.Context(), store.User{
		Provider:        idToken.Issuer,
		ProviderSubject: claims.Subject,
		Email:           claims.Email,
		Name:            claims.Name,
		PictureURL:      claims.Picture,
	})
	if err != nil {
		http.Error(w, "could not save user", http.StatusInternalServerError)
		return
	}
	encoded, err := o.encodeSession(user)
	if err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    encoded,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   o.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: o.secureCookies, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: pkceCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: o.secureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (o *OIDC) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: o.secureCookies, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: pkceCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: o.secureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (o *OIDC) allowed(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(o.allowedEmails) == 0 && len(o.allowedDomains) == 0 {
		return true
	}
	if o.allowedEmails[email] {
		return true
	}
	_, domain, ok := strings.Cut(email, "@")
	return ok && o.allowedDomains[domain]
}

type sessionPayload struct {
	ID         string    `json:"id"`
	Email      string    `json:"email,omitempty"`
	Name       string    `json:"name,omitempty"`
	PictureURL string    `json:"picture_url,omitempty"`
	Expires    time.Time `json:"expires"`
}

func (o *OIDC) encodeSession(user store.User) (string, error) {
	payload, err := json.Marshal(sessionPayload{
		ID: user.ID, Email: user.Email, Name: user.Name, PictureURL: user.PictureURL, Expires: time.Now().UTC().Add(sessionMaxAge),
	})
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := o.sign(encodedPayload)
	return encodedPayload + "." + signature, nil
}

func (o *OIDC) decodeSession(value string) (store.User, error) {
	payloadPart, signature, ok := strings.Cut(value, ".")
	if !ok || !hmac.Equal([]byte(signature), []byte(o.sign(payloadPart))) {
		return store.User{}, errors.New("invalid session")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return store.User{}, err
	}
	var payload sessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return store.User{}, err
	}
	if payload.ID == "" || time.Now().UTC().After(payload.Expires) {
		return store.User{}, errors.New("expired session")
	}
	return store.User{ID: payload.ID, Email: payload.Email, Name: payload.Name, PictureURL: payload.PictureURL}, nil
}

func (o *OIDC) sign(value string) string {
	mac := hmac.New(sha256.New, o.sessionSecret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func setFromStrings(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = true
		}
	}
	return out
}
