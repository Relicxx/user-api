// Package handler provides HTTP handlers
package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"user-api/internal/broker"
	"user-api/internal/cache"
	"user-api/internal/model"

	"github.com/go-chi/chi"
)

type UserStorage interface {
	GetUsers() ([]model.User, error)
	GetUserByID(id int) (*model.User, error)
	CreateUser(user *model.User) error
	UpdateUser(user *model.User) error
	DeleteUser(id int) error
}

type UserHandler struct {
	Storage  UserStorage
	Cache    *cache.RedisCache
	Producer *broker.KafkaProducer
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

func validateUser(user *model.User) error {
	if user.Name == "" || user.Email == "" {
		return fmt.Errorf("name and email are required")
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

	if err = h.Storage.CreateUser(&user); err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	if err := h.Producer.PublishUserCreated(r.Context(), &user); err != nil {
		log.Printf("failed to publish user-created event: %v", err)
	}

	responseWithJSON(w, http.StatusCreated, map[string]string{"message": "User created successfully"})
}

func (h *UserHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.Storage.GetUsers()
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

	user, err := h.Storage.GetUserByID(id)
	if err == sql.ErrNoRows {
		errorWithJSON(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to receive user")
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

	err = h.Storage.UpdateUser(&user)
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	responseWithJSON(w, http.StatusOK, map[string]string{"message": "User updated successfully"})
}

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		errorWithJSON(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	err = h.Storage.DeleteUser(id)
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	responseWithJSON(w, http.StatusOK, map[string]string{"message": "User deleted successfully"})
}
