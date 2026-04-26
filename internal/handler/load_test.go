package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"user-api/internal/model"

	"github.com/go-chi/chi"
)

func TestLoad(t *testing.T) {
	storage := &mockStorage{
		users: []model.User{
			{ID: 1, Name: "Alice", Email: "alice@example.com"},
			{ID: 2, Name: "Bob", Email: "bob@example.com"},
			{ID: 3, Name: "Charlie", Email: "charlie@example.com"},
		},
	}

	cache := newMemCache()
	data, _ := json.Marshal(&storage.users[0])
	cache.Set(context.Background(), "user:1", data, 5*time.Minute)

	h := &UserHandler{
		Storage:  storage,
		Cache:    cache,
		Producer: noopProducer{},
	}

	router := chi.NewRouter()
	router.Get("/users", h.GetUsers)
	router.Get("/users/{id}", h.GetUserByID)

	run := func(name string, makeReq func() *http.Request, workers int, duration time.Duration) {
		var total, success, failed int64

		done := make(chan struct{})
		time.AfterFunc(duration, func() { close(done) })

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-done:
						return
					default:
						req := makeReq()
						w := httptest.NewRecorder()
						router.ServeHTTP(w, req)
						atomic.AddInt64(&total, 1)
						if w.Code == http.StatusOK {
							atomic.AddInt64(&success, 1)
						} else {
							atomic.AddInt64(&failed, 1)
						}
					}
				}
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		fmt.Printf("\n[%s | workers=%d | duration=%s]\n", name, workers, duration)
		fmt.Printf("  Requests: %d | Success: %d | Failed: %d\n", total, success, failed)
		fmt.Printf("  RPS:      %.0f req/s\n", float64(total)/elapsed.Seconds())

		if failed > 0 {
			t.Errorf("%s: got %d failed requests", name, failed)
		}
	}

	run(
		"GET /users",
		func() *http.Request { return httptest.NewRequest(http.MethodGet, "/users", nil) },
		50, 5*time.Second,
	)

	run(
		"GET /users/{id} cache-hit",
		func() *http.Request { return httptest.NewRequest(http.MethodGet, "/users/1", nil) },
		50, 5*time.Second,
	)

	run(
		"GET /users/{id} cache-miss",
		func() *http.Request {
			h.Cache = newMemCache()
			return httptest.NewRequest(http.MethodGet, "/users/1", nil)
		},
		1, 5*time.Second,
	)
}
