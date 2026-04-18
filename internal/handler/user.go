// Package handler provides HTTP handlers
package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"user-api/internal/db"
	"user-api/internal/model"

	"github.com/go-chi/chi"
)

type UserHandler struct {
	DB *sql.DB
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

	if err = db.CreateUser(h.DB, &user); err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	responseWithJSON(w, http.StatusCreated, map[string]string{"message": "User created successfully"})
}

func (h *UserHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := db.GetUsers(h.DB)
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

	user, err := db.GetUserByID(h.DB, id)
	if err == sql.ErrNoRows {
		errorWithJSON(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to receive user")
		return
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

	err = db.UpdateUser(h.DB, &user)
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

	err = db.DeleteUser(h.DB, id)
	if err != nil {
		errorWithJSON(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	responseWithJSON(w, http.StatusOK, map[string]string{"message": "User deleted successfully"})
}
