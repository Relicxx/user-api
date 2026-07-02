package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"user-api/internal/model"
)

type mockStorage struct {
	users []model.User
}

func (m *mockStorage) GetUsers(_ context.Context) ([]model.User, error) {
	return m.users, nil
}

func (m *mockStorage) GetUserByID(_ context.Context, id int) (*model.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return &u, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) CreateUser(_ context.Context, user *model.User) error {
	return nil
}

func (m *mockStorage) UpdateUser(_ context.Context, user *model.User) error {
	for _, u := range m.users {
		if u.ID == user.ID {
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockStorage) DeleteUser(_ context.Context, id int) error {
	for _, u := range m.users {
		if u.ID == id {
			return nil
		}
	}
	return sql.ErrNoRows
}

func TestGetUsers(t *testing.T) {
	storage := &mockStorage{
		users: []model.User{
			{ID: 1, Name: "Alice", Email: "alice@example.com"},
		},
	}

	h := &UserHandler{Storage: storage}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()

	h.GetUsers(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCreateUser(t *testing.T) {
	storage := &mockStorage{}
	h := &UserHandler{Storage: storage, Producer: noopProducer{}}

	tests := []struct {
		body string
		want int
	}{
		{`{"name": "Bob", "email": "bob@example.com"}`, http.StatusCreated},
		{`{"name": "", "email": "bob@example.com"}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		body := strings.NewReader(tt.body)
		req := httptest.NewRequest(http.MethodPost, "/users", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateUser(w, req)

		if w.Code != tt.want {
			t.Errorf("expected %d, got %d", tt.want, w.Code)
		}
	}
}
