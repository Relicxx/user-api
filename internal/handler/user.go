package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"user-api/internal/cache"
	"user-api/internal/db"
	"user-api/internal/model"

	"github.com/go-chi/chi/v5"
)

type UserStorage interface {
	GetUsers(ctx context.Context, limit, offset int) ([]model.User, error)
	GetUserByID(ctx context.Context, id int) (*model.User, error)
	CreateUser(ctx context.Context, user *model.User) error
	UpdateUser(ctx context.Context, user *model.User) error
	DeleteUser(ctx context.Context, id int) error
}

type Cache interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	Del(ctx context.Context, key string) error
}

type UserHandler struct {
	Storage UserStorage
	Cache   Cache
}

const (
	cacheTTL     = 5 * time.Minute
	maxBodyBytes = 1 << 20 // 1 MiB
)

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return json.NewDecoder(r.Body).Decode(dst)
}

func responseWithJSON(w http.ResponseWriter, statuscode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statuscode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func errorWithJSON(w http.ResponseWriter, statuscode int, message string) {
	responseWithJSON(w, statuscode, map[string]string{"error": message})
}

func parseID(r *http.Request) (int, error) {
	strID := chi.URLParam(r, "id")
	return strconv.Atoi(strID)
}

const (
	maxNameLength  = 100
	maxEmailLength = 254
)

var emailRegexp = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func validateUser(user *model.User) error {
	user.Name = strings.TrimSpace(user.Name)
	user.Email = strings.TrimSpace(user.Email)

	if user.Name == "" || user.Email == "" {
		return fmt.Errorf("name and email are required")
	}
	if utf8.RuneCountInString(user.Name) > maxNameLength {
		return fmt.Errorf("name must be at most %d characters", maxNameLength)
	}
	if len(user.Email) > maxEmailLength || !emailRegexp.MatchString(user.Email) {
		return fmt.Errorf("invalid email format")
	}

	return nil
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var user model.User
	err := decodeBody(w, r, &user)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			errorWithJSON(w, http.StatusRequestEntityTooLarge, "Request body too large")
			return
		}
		errorWithJSON(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := validateUser(&user); err != nil {
		errorWithJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	err = h.Storage.CreateUser(r.Context(), &user)
	if errors.Is(err, db.ErrDuplicateEmail) {
		errorWithJSON(w, http.StatusConflict, "Email already exists")
		return
	}
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	responseWithJSON(w, http.StatusCreated, user)
}

const (
	defaultLimit = 50
	maxLimit     = 100
)

func parseLimitOffset(r *http.Request) (limit, offset int, err error) {
	limit, offset = defaultLimit, 0

	if v := r.URL.Query().Get("limit"); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil || n < 1 {
			return 0, 0, fmt.Errorf("limit must be a positive integer")
		}
		if n > maxLimit {
			n = maxLimit
		}
		limit = n
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil || n < 0 {
			return 0, 0, fmt.Errorf("offset must be a non-negative integer")
		}
		offset = n
	}

	return limit, offset, nil
}

func (h *UserHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r)
	if err != nil {
		errorWithJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	users, err := h.Storage.GetUsers(r.Context(), limit, offset)
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to receive users")
		return
	}
	if users == nil {
		users = []model.User{}
	}

	responseWithJSON(w, http.StatusOK, users)
}

func (h *UserHandler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errorWithJSON(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	key := fmt.Sprintf("user:%d", id)

	if h.Cache != nil {
		cached, cacheErr := h.Cache.Get(r.Context(), key)
		switch {
		case cacheErr == nil:
			var user model.User
			unmarshalErr := json.Unmarshal(cached, &user)
			if unmarshalErr == nil {
				responseWithJSON(w, http.StatusOK, &user)
				return
			}
			slog.Warn("corrupted cache entry, falling back to db", "key", key, "error", unmarshalErr)
		case !errors.Is(cacheErr, cache.ErrCacheMiss):
			slog.Error("cache get failed", "key", key, "error", cacheErr)
		}
	}

	user, err := h.Storage.GetUserByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		errorWithJSON(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to receive user")
		return
	}
	if user == nil {
		errorWithJSON(w, http.StatusNotFound, "User not found")
		return
	}

	if h.Cache != nil {
		data, marshalErr := json.Marshal(user)
		if marshalErr != nil {
			slog.Error("failed to marshal user for cache", "user_id", id, "error", marshalErr)
		} else if setErr := h.Cache.Set(r.Context(), key, data, cacheTTL); setErr != nil {
			slog.Error("cache set failed", "key", key, "error", setErr)
		}
	}

	responseWithJSON(w, http.StatusOK, user)
}

func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errorWithJSON(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var user model.User
	err = decodeBody(w, r, &user)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			errorWithJSON(w, http.StatusRequestEntityTooLarge, "Request body too large")
			return
		}
		errorWithJSON(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := validateUser(&user); err != nil {
		errorWithJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	user.ID = id

	err = h.Storage.UpdateUser(r.Context(), &user)
	if errors.Is(err, sql.ErrNoRows) {
		errorWithJSON(w, http.StatusNotFound, "User not found")
		return
	}
	if errors.Is(err, db.ErrDuplicateEmail) {
		errorWithJSON(w, http.StatusConflict, "Email already exists")
		return
	}
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	h.invalidateUser(r.Context(), id)

	responseWithJSON(w, http.StatusOK, map[string]string{"message": "User updated successfully"})
}

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errorWithJSON(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	err = h.Storage.DeleteUser(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		errorWithJSON(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	h.invalidateUser(r.Context(), id)

	responseWithJSON(w, http.StatusOK, map[string]string{"message": "User deleted successfully"})
}

// invalidateUser drops the user from the cache after an update or delete
// so cache-aside reads never serve stale data.
func (h *UserHandler) invalidateUser(ctx context.Context, id int) {
	if h.Cache == nil {
		return
	}
	if err := h.Cache.Del(ctx, fmt.Sprintf("user:%d", id)); err != nil {
		slog.Error("failed to invalidate cache", "user_id", id, "error", err)
	}
}
