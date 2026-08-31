package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JWTAuth rejects requests that do not carry a valid HS256 bearer token.
// It guards the mutating endpoints; read endpoints and probes stay open.
type JWTAuth struct {
	secret []byte
	log    *slog.Logger
}

// NewJWTAuth builds the middleware for the given signing secret; a nil
// logger falls back to slog.Default.
func NewJWTAuth(secret []byte, log *slog.Logger) *JWTAuth {
	if log == nil {
		log = slog.Default()
	}
	return &JWTAuth{secret: secret, log: log}
}

// Handler is the middleware entry point.
func (a *JWTAuth) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			a.unauthorized(w, r, `Bearer realm="user-api"`, "missing bearer token")
			return
		}

		_, err := jwt.Parse(token, a.key,
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithExpirationRequired(),
		)
		if err != nil {
			a.unauthorized(w, r, `Bearer realm="user-api", error="invalid_token"`, err.Error())
			return
		}

		next.ServeHTTP(w, r)
	})
}

// key hands the signing secret to the parser; the accepted algorithms are
// already pinned to HS256 via jwt.WithValidMethods.
func (a *JWTAuth) key(_ *jwt.Token) (any, error) {
	return a.secret, nil
}

func (a *JWTAuth) unauthorized(w http.ResponseWriter, r *http.Request, challenge, reason string) {
	a.log.LogAttrs(r.Context(), slog.LevelWarn, "unauthorized request",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("reason", reason),
	)
	w.Header().Set("WWW-Authenticate", challenge)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
}

// bearerToken extracts the token from the Authorization header. The scheme
// is matched case-insensitively, as RFC 7235 requires.
func bearerToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return "", false
	}
	return auth[len(prefix):], true
}
