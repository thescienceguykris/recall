package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"recall/internal/store"

	"golang.org/x/oauth2"
)

type fakeUserStore struct{}

func (fakeUserStore) EnsureUser(context.Context, store.User) (store.User, error) {
	return store.User{}, nil
}

func TestLoginUsesPKCE(t *testing.T) {
	authURL, err := url.Parse("https://auth.example/authorize")
	if err != nil {
		t.Fatal(err)
	}
	svc := &OIDC{
		store: fakeUserStore{},
		oauth: oauth2.Config{
			ClientID:    "client-id",
			Endpoint:    oauth2.Endpoint{AuthURL: authURL.String(), TokenURL: "https://auth.example/token"},
			RedirectURL: "https://recall.example/auth/callback",
			Scopes:      []string{"openid", "profile", "email"},
		},
		sessionSecret: []byte("01234567890123456789012345678901"),
		secureCookies: true,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	svc.Login(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	redirectURL, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	query := redirectURL.Query()
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("expected S256 PKCE challenge method, got %q", query.Get("code_challenge_method"))
	}
	if query.Get("code_challenge") == "" {
		t.Fatal("expected PKCE code challenge")
	}
	if query.Get("state") == "" {
		t.Fatal("expected oauth state")
	}

	cookies := rec.Result().Cookies()
	if findCookie(cookies, stateCookie) == nil {
		t.Fatal("expected state cookie")
	}
	pkce := findCookie(cookies, pkceCookie)
	if pkce == nil {
		t.Fatal("expected PKCE verifier cookie")
	}
	if !pkce.HttpOnly || !pkce.Secure {
		t.Fatal("expected secure, httponly PKCE verifier cookie")
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
