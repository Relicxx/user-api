package middleware

import (
	"log/slog"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// RequestLogger writes one structured slog record per request, so access
// logs land in the same JSON stream as application logs and can be
// correlated by request_id (set by chi's RequestID middleware). The ID is
// also echoed in the X-Request-ID response header so clients can report it.
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

			reqID := chimw.GetReqID(r.Context())
			if reqID != "" {
				ww.Header().Set("X-Request-ID", reqID)
			}

			next.ServeHTTP(ww, r)

			log.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("request_id", reqID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}
