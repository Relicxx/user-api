// Package metrics defines the service's Prometheus instrumentation:
// HTTP request metrics collected by a chi middleware and outbox publish
// counters collected by a publisher decorator.
package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"user-api/internal/outbox"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests processed, by method, route and status code.",
	}, []string{"method", "route", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, by method and route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	httpRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Number of HTTP requests currently being served.",
	})

	outboxPublishedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_events_published_total",
		Help: "Total number of outbox events successfully published to the broker.",
	})

	outboxPublishErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_publish_errors_total",
		Help: "Total number of failed outbox publish attempts.",
	})
)

// Handler exposes the metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Middleware records request count, latency and in-flight gauge for every
// request. The route label uses the chi route pattern (e.g. /users/{id}),
// not the raw URL, to keep label cardinality bounded.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		next.ServeHTTP(ww, r)

		route := "unmatched"
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if pattern := rctx.RoutePattern(); pattern != "" {
				route = pattern
			}
		}

		httpRequestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(ww.Status())).Inc()
		httpRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// InstrumentedPublisher decorates an outbox.Publisher with publish counters,
// keeping the outbox package itself free of metrics concerns.
type InstrumentedPublisher struct {
	Next outbox.Publisher
}

func (p *InstrumentedPublisher) Publish(ctx context.Context, topic, key string, payload []byte) error {
	err := p.Next.Publish(ctx, topic, key, payload)
	if err != nil {
		outboxPublishErrorsTotal.Inc()
		return err
	}
	outboxPublishedTotal.Inc()
	return nil
}
