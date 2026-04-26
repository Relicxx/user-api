package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"user-api/internal/model"
)

// --- in-memory cache mock (потокобезопасна через RWMutex) ---

type memCache struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newMemCache() *memCache {
	return &memCache{data: make(map[string][]byte)}
}

func (m *memCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *memCache) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, &cacheMiss{}
	}
	return v, nil
}

type cacheMiss struct{}

func (cacheMiss) Error() string { return "cache miss" }

// --- no-op producer mock ---

type noopProducer struct{}

func (noopProducer) PublishUserCreated(_ context.Context, _ *model.User) error { return nil }

// --- BenchmarkGetUsers ---
// Мерим чистую скорость хэндлера: JSON-сериализация + HTTP overhead.
// База данных заменена моком, поэтому это throughput самого хэндлера.

func BenchmarkGetUsers(b *testing.B) {
	storage := &mockStorage{
		users: []model.User{
			{ID: 1, Name: "Alice", Email: "alice@example.com"},
			{ID: 2, Name: "Bob", Email: "bob@example.com"},
			{ID: 3, Name: "Charlie", Email: "charlie@example.com"},
		},
	}
	h := &UserHandler{Storage: storage}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/users", nil)
		w := httptest.NewRecorder()
		h.GetUsers(w, req)
	}
}

// --- BenchmarkGetUserByID_CacheHit ---
// Мерим путь cache-hit: Redis нашёл — вернули. БД не трогаем.

func BenchmarkGetUserByID_CacheHit(b *testing.B) {
	cache := newMemCache()
	user := model.User{ID: 1, Name: "Alice", Email: "alice@example.com"}
	data, _ := json.Marshal(&user)
	cache.Set(context.Background(), "user:1", data, 5*time.Minute)

	h := &UserHandler{
		Storage: &mockStorage{},
		Cache:   cache,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
		w := httptest.NewRecorder()
		h.GetUserByID(w, req)
	}
}

// --- BenchmarkGetUserByID_CacheMiss ---
// Мерим путь cache-miss: кэш пуст → мок-storage → записать в кэш → вернуть.

func BenchmarkGetUserByID_CacheMiss(b *testing.B) {
	storage := &mockStorage{}
	storage.users = []model.User{{ID: 1, Name: "Alice", Email: "alice@example.com"}}

	h := &UserHandler{
		Storage: storage,
		Cache:   newMemCache(),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		h.Cache = newMemCache() // сбрасываем кэш на каждой итерации
		req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
		w := httptest.NewRecorder()
		h.GetUserByID(w, req)
	}
}

// --- BenchmarkCreateUser ---
// Мерим: decode JSON → validate → mock insert → publish (noop).

func BenchmarkCreateUser(b *testing.B) {
	h := &UserHandler{
		Storage:  &mockStorage{},
		Producer: noopProducer{},
	}

	body := `{"name":"Alice","email":"alice@example.com"}`

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.CreateUser(w, req)
	}
}
