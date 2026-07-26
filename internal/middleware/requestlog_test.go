package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
)

func TestRequestLoggerEmitsStructuredRecord(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	})
	h = RequestLogger(log)(h)
	h = chimw.RequestID(h)

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got == "" {
		t.Error("expected X-Request-ID response header to be set")
	}

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log output is not valid json: %v (%q)", err, buf.String())
	}

	if rec["msg"] != "http request" {
		t.Errorf("unexpected log message: %v", rec["msg"])
	}
	if rec["method"] != "GET" || rec["path"] != "/users/42" {
		t.Errorf("unexpected method/path: %v %v", rec["method"], rec["path"])
	}
	if rec["status"] != float64(http.StatusNotFound) {
		t.Errorf("expected status 404 in log, got %v", rec["status"])
	}
	if rec["bytes"] != float64(4) {
		t.Errorf("expected 4 bytes written, got %v", rec["bytes"])
	}
	if rec["request_id"] == "" || rec["request_id"] == nil {
		t.Error("expected request_id in log record")
	}
	if rec["remote_addr"] != "10.0.0.1:1234" {
		t.Errorf("unexpected remote_addr: %v", rec["remote_addr"])
	}
}

func TestRequestLoggerWithoutRequestID(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	h := RequestLogger(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if got := w.Header().Get("X-Request-ID"); got != "" {
		t.Errorf("expected no X-Request-ID header without RequestID middleware, got %q", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"status":200`)) {
		t.Errorf("expected status 200 in log record, got %q", buf.String())
	}
}
