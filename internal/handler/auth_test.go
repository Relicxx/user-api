package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

func newAuthRouter() *chi.Mux {
	h := &AuthHandler{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		JWTSecret:    []byte("signing-secret"),
		TokenTTL:     15 * time.Minute,
	}
	r := chi.NewRouter()
	r.Post("/auth/token", h.Token)
	return r
}

func TestTokenValidCredentials(t *testing.T) {
	router := newAuthRouter()

	w := doRequest(t, router, http.MethodPost, "/auth/token",
		`{"client_id": "test-client", "client_secret": "test-secret"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp tokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("expected token_type Bearer, got %q", resp.TokenType)
	}
	if resp.ExpiresIn != int64((15 * time.Minute).Seconds()) {
		t.Errorf("expected expires_in 900, got %d", resp.ExpiresIn)
	}

	token, err := jwt.Parse(resp.AccessToken,
		func(_ *jwt.Token) (any, error) { return []byte("signing-secret"), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		t.Fatalf("issued token does not validate: %v", err)
	}
	if sub, _ := token.Claims.GetSubject(); sub != "test-client" {
		t.Errorf("expected subject test-client, got %q", sub)
	}
}

func TestTokenInvalidCredentials(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"wrong client_id", `{"client_id": "nope", "client_secret": "test-secret"}`},
		{"wrong client_secret", `{"client_id": "test-client", "client_secret": "nope"}`},
		{"empty credentials", `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newAuthRouter()

			w := doRequest(t, router, http.MethodPost, "/auth/token", tt.body)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", w.Code)
			}
			if msg := decodeError(t, w); msg != "Invalid client credentials" {
				t.Errorf("unexpected error message: %q", msg)
			}
		})
	}
}

func TestTokenMalformedBody(t *testing.T) {
	router := newAuthRouter()

	w := doRequest(t, router, http.MethodPost, "/auth/token", `{"client_id": `)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
