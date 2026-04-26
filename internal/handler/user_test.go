package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"user-api/internal/model"
)

type mockStorage struct {
	users []model.User
}

func (m *mockStorage) GetUsers() ([]model.User, error) {
	return m.users, nil
}

func (m *mockStorage) GetUserByID(id int) (*model.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return &u, nil
		}
	}
	return nil, nil
}

func (m *mockStorage) CreateUser(user *model.User) error {
	return nil
}

func (m *mockStorage) UpdateUser(user *model.User) error {
	return nil
}

func (m *mockStorage) DeleteUser(id int) error {
	return nil
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
