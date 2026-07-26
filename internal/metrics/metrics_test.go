package metrics

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func scrape(t *testing.T) string {
	t.Helper()

	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d", w.Code)
	}
	body, err := io.ReadAll(w.Body)
	if err != nil {
		t.Fatalf("failed to read metrics body: %v", err)
	}
	return string(body)
}

func TestMiddlewareRecordsRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := scrape(t)
	want := `http_requests_total{method="GET",route="/users/{id}",status="200"}`
	if !strings.Contains(body, want) {
		t.Errorf("metrics output does not contain %q", want)
	}
	if !strings.Contains(body, `http_request_duration_seconds_count{method="GET",route="/users/{id}"}`) {
		t.Errorf("metrics output does not contain duration histogram for the route")
	}
}

func TestMiddlewareRecordsStatusCodes(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/boom", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	body := scrape(t)
	want := `http_requests_total{method="GET",route="/boom",status="500"}`
	if !strings.Contains(body, want) {
		t.Errorf("metrics output does not contain %q", want)
	}
}

type fakePublisher struct {
	err error
}

func (p *fakePublisher) Publish(_ context.Context, _, _ string, _ []byte) error {
	return p.err
}

func TestInstrumentedPublisherCountsOutcomes(t *testing.T) {
	publishedBefore := testutil.ToFloat64(outboxPublishedTotal)
	errorsBefore := testutil.ToFloat64(outboxPublishErrorsTotal)

	ok := &InstrumentedPublisher{Next: &fakePublisher{}}
	if err := ok.Publish(context.Background(), "t", "k", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	failing := &InstrumentedPublisher{Next: &fakePublisher{err: errors.New("broker down")}}
	if err := failing.Publish(context.Background(), "t", "k", nil); err == nil {
		t.Fatal("expected error from failing publisher")
	}

	if got := testutil.ToFloat64(outboxPublishedTotal) - publishedBefore; got != 1 {
		t.Errorf("expected published counter +1, got +%v", got)
	}
	if got := testutil.ToFloat64(outboxPublishErrorsTotal) - errorsBefore; got != 1 {
		t.Errorf("expected error counter +1, got +%v", got)
	}
}
