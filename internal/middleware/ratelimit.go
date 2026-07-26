// Package middleware provides HTTP middleware for the service: per-IP rate
// limiting and structured request logging.
package middleware

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter applies a token-bucket limit per client IP. Requests over the
// limit get 429 with a Retry-After hint. State is in-memory, so limits are
// per instance; put a shared limiter in front if you run many replicas.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rps      rate.Limit
	burst    int
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		visitors: make(map[string]*visitor),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

// Handler is the middleware entry point.
func (rl *RateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"Too many requests"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.visitors[ip] = v
	}
	v.lastSeen = time.Now()

	return v.limiter.Allow()
}

// CleanupLoop periodically drops visitors idle longer than maxIdle so the
// map does not grow unbounded. Run it in its own goroutine; it returns when
// ctx is canceled.
func (rl *RateLimiter) CleanupLoop(ctx context.Context, every, maxIdle time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.cleanup(maxIdle)
		}
	}
}

func (rl *RateLimiter) cleanup(maxIdle time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-maxIdle)
	for ip, v := range rl.visitors {
		if v.lastSeen.Before(cutoff) {
			delete(rl.visitors, ip)
		}
	}
}

// clientIP extracts the remote IP without the port. RemoteAddr is used on
// purpose: X-Forwarded-For is only trustworthy behind a proxy that is known
// to set it, which is a deployment-specific decision.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
