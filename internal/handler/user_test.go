package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"user-api/internal/db"
	"user-api/internal/model"

	"github.com/go-chi/chi/v5"
)

type mockStorage struct {
	users     []model.User
	createErr error
	updateErr error
	deleteErr error
}

func (m *mockStorage) GetUsers(_ context.Context, limit, offset int) ([]model.User, error) {
	if offset >= len(m.users) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.users) {
		end = len(m.users)
	}
	return m.users[offset:end], nil
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
	if m.createErr != nil {
		return m.createErr
	}
	user.ID = len(m.users) + 1
	m.users = append(m.users, *user)
	return nil
}

func (m *mockStorage) UpdateUser(_ context.Context, user *model.User) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i, u := range m.users {
		if u.ID == user.ID {
			m.users[i] = *user
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockStorage) DeleteUser(_ context.Context, id int) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i, u := range m.users {
		if u.ID == id {
			m.users = append(m.users[:i], m.users[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

func newRouter(h *UserHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/users", h.GetUsers)
	r.Get("/users/{id}", h.GetUserByID)
	r.Post("/users", h.CreateUser)
	r.Put("/users/{id}", h.UpdateUser)
	r.Delete("/users/{id}", h.DeleteUser)
	return r
}

func doRequest(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeUser(t *testing.T, w *httptest.ResponseRecorder) model.User {
	t.Helper()

	var user model.User
	if err := json.NewDecoder(w.Body).Decode(&user); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return user
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	return body["error"]
}

func TestGetUsers(t *testing.T) {
	storage := &mockStorage{
		users: []model.User{
			{ID: 1, Name: "Alice", Email: "alice@example.com"},
			{ID: 2, Name: "Bob", Email: "bob@example.com"},
		},
	}
	router := newRouter(&UserHandler{Storage: storage})

	w := doRequest(t, router, http.MethodGet, "/users", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var users []model.User
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	w = doRequest(t, router, http.MethodGet, "/users?limit=1&offset=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	users = nil
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(users) != 1 || users[0].ID != 2 {
		t.Errorf("expected only user 2, got %+v", users)
	}
}

func TestGetUsersInvalidPagination(t *testing.T) {
	router := newRouter(&UserHandler{Storage: &mockStorage{}})

	for _, path := range []string{"/users?limit=abc", "/users?limit=0", "/users?offset=-1"} {
		w := doRequest(t, router, http.MethodGet, path, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", path, w.Code)
		}
	}
}

func TestGetUserByIDCacheMiss(t *testing.T) {
	storage := &mockStorage{
		users: []model.User{{ID: 1, Name: "Alice", Email: "alice@example.com"}},
	}
	cache := newMemCache()
	router := newRouter(&UserHandler{Storage: storage, Cache: cache})

	w := doRequest(t, router, http.MethodGet, "/users/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	user := decodeUser(t, w)
	if user.ID != 1 || user.Name != "Alice" || user.Email != "alice@example.com" {
		t.Errorf("unexpected user in response: %+v", user)
	}

	cached, err := cache.Get(context.Background(), "user:1")
	if err != nil {
		t.Fatalf("expected user to be cached after miss, got error: %v", err)
	}
	var cachedUser model.User
	if err := json.Unmarshal(cached, &cachedUser); err != nil {
		t.Fatalf("cached value is not valid json: %v", err)
	}
	if cachedUser != user {
		t.Errorf("cached user %+v differs from response %+v", cachedUser, user)
	}
}

func TestGetUserByIDCacheHit(t *testing.T) {
	storage := &mockStorage{
		users: []model.User{{ID: 1, Name: "FromDB", Email: "db@example.com"}},
	}
	cache := newMemCache()
	cachedUser := model.User{ID: 1, Name: "FromCache", Email: "cache@example.com"}
	data, _ := json.Marshal(&cachedUser)
	cache.Set(context.Background(), "user:1", data, 5*time.Minute)

	router := newRouter(&UserHandler{Storage: storage, Cache: cache})

	w := doRequest(t, router, http.MethodGet, "/users/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	user := decodeUser(t, w)
	if user.Name != "FromCache" {
		t.Errorf("expected cached user to be served, got %+v", user)
	}
}

func TestGetUserByIDCorruptedCache(t *testing.T) {
	storage := &mockStorage{
		users: []model.User{{ID: 1, Name: "Alice", Email: "alice@example.com"}},
	}
	cache := newMemCache()
	cache.Set(context.Background(), "user:1", []byte("{not json"), 5*time.Minute)

	router := newRouter(&UserHandler{Storage: storage, Cache: cache})

	w := doRequest(t, router, http.MethodGet, "/users/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 fallback to db, got %d", w.Code)
	}

	user := decodeUser(t, w)
	if user.Name != "Alice" {
		t.Errorf("expected db user after corrupted cache, got %+v", user)
	}
}

func TestGetUserByIDNotFound(t *testing.T) {
	router := newRouter(&UserHandler{Storage: &mockStorage{}, Cache: newMemCache()})

	w := doRequest(t, router, http.MethodGet, "/users/99", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if msg := decodeError(t, w); msg != "User not found" {
		t.Errorf("unexpected error message: %q", msg)
	}
}

func TestGetUserByIDInvalidID(t *testing.T) {
	router := newRouter(&UserHandler{Storage: &mockStorage{}, Cache: newMemCache()})

	w := doRequest(t, router, http.MethodGet, "/users/abc", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if msg := decodeError(t, w); msg != "Invalid user ID" {
		t.Errorf("unexpected error message: %q", msg)
	}
}

func TestCreateUser(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", `{"name": "Bob", "email": "bob@example.com"}`, http.StatusCreated},
		{"empty name", `{"name": "", "email": "bob@example.com"}`, http.StatusBadRequest},
		{"whitespace name", `{"name": "   ", "email": "bob@example.com"}`, http.StatusBadRequest},
		{"invalid email", `{"name": "Bob", "email": "not-an-email"}`, http.StatusBadRequest},
		{"malformed json", `{"name": `, http.StatusBadRequest},
		{"name too long", `{"name": "` + strings.Repeat("a", 101) + `", "email": "bob@example.com"}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouter(&UserHandler{Storage: &mockStorage{}})

			w := doRequest(t, router, http.MethodPost, "/users", tt.body)
			if w.Code != tt.want {
				t.Errorf("expected %d, got %d", tt.want, w.Code)
			}
		})
	}
}

func TestCreateUserReturnsID(t *testing.T) {
	router := newRouter(&UserHandler{Storage: &mockStorage{}})

	w := doRequest(t, router, http.MethodPost, "/users", `{"name": "Bob", "email": "bob@example.com"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	user := decodeUser(t, w)
	if user.ID != 1 {
		t.Errorf("expected assigned id 1, got %d", user.ID)
	}
	if user.Name != "Bob" || user.Email != "bob@example.com" {
		t.Errorf("unexpected user in response: %+v", user)
	}
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	storage := &mockStorage{createErr: db.ErrDuplicateEmail}
	router := newRouter(&UserHandler{Storage: storage})

	w := doRequest(t, router, http.MethodPost, "/users", `{"name": "Bob", "email": "bob@example.com"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if msg := decodeError(t, w); msg != "Email already exists" {
		t.Errorf("unexpected error message: %q", msg)
	}
}

func TestUpdateUser(t *testing.T) {
	storage := &mockStorage{
		users: []model.User{{ID: 1, Name: "Alice", Email: "alice@example.com"}},
	}
	cache := newMemCache()
	cache.Set(context.Background(), "user:1", []byte(`{"id":1}`), 5*time.Minute)

	router := newRouter(&UserHandler{Storage: storage, Cache: cache})

	w := doRequest(t, router, http.MethodPut, "/users/1", `{"name": "Alice Smith", "email": "alice@example.com"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if storage.users[0].Name != "Alice Smith" {
		t.Errorf("expected storage to be updated, got %+v", storage.users[0])
	}
	if _, err := cache.Get(context.Background(), "user:1"); err == nil {
		t.Errorf("expected cache to be invalidated after update")
	}
}

func TestUpdateUserErrors(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    string
		storage *mockStorage
		want    int
	}{
		{
			name:    "not found",
			path:    "/users/99",
			body:    `{"name": "Bob", "email": "bob@example.com"}`,
			storage: &mockStorage{},
			want:    http.StatusNotFound,
		},
		{
			name:    "invalid id",
			path:    "/users/abc",
			body:    `{"name": "Bob", "email": "bob@example.com"}`,
			storage: &mockStorage{},
			want:    http.StatusBadRequest,
		},
		{
			name:    "invalid body",
			path:    "/users/1",
			body:    `{"name": `,
			storage: &mockStorage{users: []model.User{{ID: 1, Name: "A", Email: "a@a.com"}}},
			want:    http.StatusBadRequest,
		},
		{
			name:    "duplicate email",
			path:    "/users/1",
			body:    `{"name": "Bob", "email": "taken@example.com"}`,
			storage: &mockStorage{updateErr: db.ErrDuplicateEmail},
			want:    http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouter(&UserHandler{Storage: tt.storage, Cache: newMemCache()})

			w := doRequest(t, router, http.MethodPut, tt.path, tt.body)
			if w.Code != tt.want {
				t.Errorf("expected %d, got %d", tt.want, w.Code)
			}
		})
	}
}

func TestDeleteUser(t *testing.T) {
	storage := &mockStorage{
		users: []model.User{{ID: 1, Name: "Alice", Email: "alice@example.com"}},
	}
	cache := newMemCache()
	cache.Set(context.Background(), "user:1", []byte(`{"id":1}`), 5*time.Minute)

	router := newRouter(&UserHandler{Storage: storage, Cache: cache})

	w := doRequest(t, router, http.MethodDelete, "/users/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(storage.users) != 0 {
		t.Errorf("expected user to be deleted from storage")
	}
	if _, err := cache.Get(context.Background(), "user:1"); err == nil {
		t.Errorf("expected cache to be invalidated after delete")
	}
}

func TestDeleteUserNotFound(t *testing.T) {
	router := newRouter(&UserHandler{Storage: &mockStorage{}, Cache: newMemCache()})

	w := doRequest(t, router, http.MethodDelete, "/users/99", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if msg := decodeError(t, w); msg != "User not found" {
		t.Errorf("unexpected error message: %q", msg)
	}
}

func TestDeleteUserInvalidID(t *testing.T) {
	router := newRouter(&UserHandler{Storage: &mockStorage{}, Cache: newMemCache()})

	w := doRequest(t, router, http.MethodDelete, "/users/abc", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
