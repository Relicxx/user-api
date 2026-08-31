package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AuthHandler serves POST /auth/token, exchanging client credentials for a
// short-lived HS256 bearer token accepted by the mutating endpoints.
type AuthHandler struct {
	ClientID     string
	ClientSecret string
	JWTSecret    []byte
	TokenTTL     time.Duration
}

type tokenRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// Token handles POST /auth/token: validates the client credentials in
// constant time and issues a signed JWT with the configured TTL.
func (h *AuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	err := decodeBody(w, r, &req)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			errorWithJSON(w, http.StatusRequestEntityTooLarge, "Request body too large")
			return
		}
		errorWithJSON(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Both fields are always compared so a mismatched client_id does not
	// return faster than a mismatched client_secret.
	idOK := constantTimeEqual(req.ClientID, h.ClientID)
	secretOK := constantTimeEqual(req.ClientSecret, h.ClientSecret)
	if !idOK || !secretOK {
		slog.Warn("token request with invalid client credentials", "client_id", req.ClientID)
		errorWithJSON(w, http.StatusUnauthorized, "Invalid client credentials")
		return
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   h.ClientID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(h.TokenTTL)),
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(h.JWTSecret)
	if err != nil {
		slog.Error("failed to sign access token", "error", err)
		errorWithJSON(w, http.StatusInternalServerError, "Failed to issue token")
		return
	}

	responseWithJSON(w, http.StatusOK, tokenResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(h.TokenTTL.Seconds()),
	})
}

// constantTimeEqual compares two strings in constant time. Hashing first
// keeps the comparison length-independent, so input length leaks nothing.
func constantTimeEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
