package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"user-api/internal/db"
	"user-api/internal/model"

	"github.com/go-chi/chi/v5"
)

type UserStorage interface {
	GetUsers(ctx context.Context) ([]model.User, error)
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

type Producer interface {
	PublishUserCreated(ctx context.Context, user *model.User) error
}

type UserHandler struct {
	Storage  UserStorage
	Cache    Cache
	Producer Producer
}

func responseWithJSON(w http.ResponseWriter, statuscode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statuscode)
	json.NewEncoder(w).Encode(data)
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
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
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

	if err := h.Producer.PublishUserCreated(r.Context(), &user); err != nil {
		log.Printf("failed to publish user-created event: %v", err)
	}

	responseWithJSON(w, http.StatusCreated, user)
}

func (h *UserHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.Storage.GetUsers(r.Context())
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to receive users")
		return
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

	cached, err := h.Cache.Get(r.Context(), key)
	if err == nil {
		var user model.User
		json.Unmarshal(cached, &user)
		responseWithJSON(w, http.StatusOK, &user)
		return
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

	data, _ := json.Marshal(user)
	h.Cache.Set(r.Context(), key, data, 5*time.Minute)

	responseWithJSON(w, http.StatusOK, user)
}

func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errorWithJSON(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var user model.User
	err = json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
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

// invalidateUser удаляет пользователя из кеша после изменения/удаления,
// чтобы cache-aside не отдавал устаревшие данные.
func (h *UserHandler) invalidateUser(ctx context.Context, id int) {
	if h.Cache == nil {
		return
	}
	if err := h.Cache.Del(ctx, fmt.Sprintf("user:%d", id)); err != nil {
		log.Printf("failed to invalidate cache for user %d: %v", id, err)
	}
}
