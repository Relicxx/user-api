package handler

import (
	"context"
	"net/http"
	"time"
)

const readyCheckTimeout = 2 * time.Second

// Pinger is implemented by dependencies that can report their liveness.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves the liveness and readiness probes.
type HealthHandler struct {
	DB    Pinger
	Cache Pinger
}

// Healthz is the liveness probe: it only confirms the process is serving.
func (h *HealthHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	responseWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz is the readiness probe: it pings PostgreSQL and Redis and returns
// 503 with per-dependency details when any of them is down.
func (h *HealthHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyCheckTimeout)
	defer cancel()

	checks := map[string]string{}
	ready := true

	if err := h.DB.Ping(ctx); err != nil {
		checks["postgres"] = err.Error()
		ready = false
	} else {
		checks["postgres"] = "ok"
	}

	if err := h.Cache.Ping(ctx); err != nil {
		checks["redis"] = err.Error()
		ready = false
	} else {
		checks["redis"] = "ok"
	}

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}

	responseWithJSON(w, status, checks)
}
