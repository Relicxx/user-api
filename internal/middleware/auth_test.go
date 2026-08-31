package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

var jwtTestSecret = []byte("test-secret")

func signToken(t *testing.T, secret []byte, method jwt.SigningMethod, claims jwt.Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(method, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return token
}

func validClaims(ttl time.Duration) jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Subject:   "test-client",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
}

func doAuthRequest(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestJWTAuthValidToken(t *testing.T) {
	h := NewJWTAuth(jwtTestSecret, nil).Handler(okHandler())

	token := signToken(t, jwtTestSecret, jwt.SigningMethodHS256, validClaims(time.Minute))
	if w := doAuthRequest(h, http.MethodPost, "/users", token); w.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", w.Code)
	}
}

func TestJWTAuthMissingToken(t *testing.T) {
	h := NewJWTAuth(jwtTestSecret, nil).Handler(okHandler())

	w := doAuthRequest(h, http.MethodPost, "/users", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
		t.Errorf("expected Bearer challenge in WWW-Authenticate, got %q", got)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected json error body, got Content-Type %q", ct)
	}
}

func TestJWTAuthInvalidToken(t *testing.T) {
	h := NewJWTAuth(jwtTestSecret, nil).Handler(okHandler())

	tests := []struct {
		name  string
		token string
	}{
		{"garbage", "not-a-jwt"},
		{"wrong secret", signToken(t, []byte("other-secret"), jwt.SigningMethodHS256, validClaims(time.Minute))},
		{"expired", signToken(t, jwtTestSecret, jwt.SigningMethodHS256, validClaims(-time.Minute))},
		{"no expiration", signToken(t, jwtTestSecret, jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "test-client"})},
		{"none algorithm", func() string {
			token, err := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims(time.Minute)).
				SignedString(jwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatalf("failed to sign token: %v", err)
			}
			return token
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doAuthRequest(h, http.MethodPost, "/users", tt.token)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", w.Code)
			}
			if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_token"`) {
				t.Errorf("expected invalid_token challenge, got %q", got)
			}
		})
	}
}

// TestJWTAuthProtectsOnlyMutatingRoutes mirrors the main.go routing: reads
// stay open while mutating endpoints demand a token.
func TestJWTAuthProtectsOnlyMutatingRoutes(t *testing.T) {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Get("/", ok)
		r.Get("/{id}", ok)
		r.Group(func(r chi.Router) {
			r.Use(NewJWTAuth(jwtTestSecret, nil).Handler)
			r.Post("/", ok)
			r.Put("/{id}", ok)
			r.Delete("/{id}", ok)
		})
	})

	for _, path := range []string{"/users", "/users/1"} {
		if w := doAuthRequest(r, http.MethodGet, path, ""); w.Code != http.StatusOK {
			t.Errorf("GET %s: expected 200 without token, got %d", path, w.Code)
		}
	}

	protected := []struct {
		method, path string
	}{
		{http.MethodPost, "/users"},
		{http.MethodPut, "/users/1"},
		{http.MethodDelete, "/users/1"},
	}
	token := signToken(t, jwtTestSecret, jwt.SigningMethodHS256, validClaims(time.Minute))
	for _, tt := range protected {
		if w := doAuthRequest(r, tt.method, tt.path, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401 without token, got %d", tt.method, tt.path, w.Code)
		}
		if w := doAuthRequest(r, tt.method, tt.path, token); w.Code != http.StatusOK {
			t.Errorf("%s %s: expected 200 with token, got %d", tt.method, tt.path, w.Code)
		}
	}
}
