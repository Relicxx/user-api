package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func doRequest(h http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestRateLimiterAllowsWithinBurst(t *testing.T) {
	h := NewRateLimiter(1, 3).Handler(okHandler())

	for i := 0; i < 3; i++ {
		if w := doRequest(h, "10.0.0.1:1234"); w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimiterRejectsOverBurst(t *testing.T) {
	h := NewRateLimiter(1, 2).Handler(okHandler())

	doRequest(h, "10.0.0.1:1234")
	doRequest(h, "10.0.0.1:1234")

	w := doRequest(h, "10.0.0.1:1234")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after burst exhausted, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429")
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected json error body, got Content-Type %q", ct)
	}
}

func TestRateLimiterIsolatesClients(t *testing.T) {
	h := NewRateLimiter(1, 1).Handler(okHandler())

	if w := doRequest(h, "10.0.0.1:1234"); w.Code != http.StatusOK {
		t.Fatalf("first client: expected 200, got %d", w.Code)
	}
	if w := doRequest(h, "10.0.0.1:9999"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("same IP, other port: expected 429, got %d", w.Code)
	}
	if w := doRequest(h, "10.0.0.2:1234"); w.Code != http.StatusOK {
		t.Fatalf("different IP: expected 200, got %d", w.Code)
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	h := NewRateLimiter(100, 1).Handler(okHandler())

	doRequest(h, "10.0.0.1:1234")
	if w := doRequest(h, "10.0.0.1:1234"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 right after burst, got %d", w.Code)
	}

	time.Sleep(20 * time.Millisecond) // 100 rps refills a token in 10ms

	if w := doRequest(h, "10.0.0.1:1234"); w.Code != http.StatusOK {
		t.Fatalf("expected 200 after refill, got %d", w.Code)
	}
}

func TestRateLimiterCleanupDropsIdleVisitors(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	h := rl.Handler(okHandler())

	doRequest(h, "10.0.0.1:1234")
	doRequest(h, "10.0.0.2:1234")

	rl.mu.Lock()
	rl.visitors["10.0.0.1"].lastSeen = time.Now().Add(-time.Hour)
	rl.mu.Unlock()

	rl.cleanup(10 * time.Minute)

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if _, ok := rl.visitors["10.0.0.1"]; ok {
		t.Error("expected idle visitor to be removed")
	}
	if _, ok := rl.visitors["10.0.0.2"]; !ok {
		t.Error("expected recent visitor to be kept")
	}
}
