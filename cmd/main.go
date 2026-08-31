// The user-api server: REST user CRUD backed by PostgreSQL, with Redis
// cache-aside reads and Kafka events delivered via a transactional outbox.
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"user-api/internal/broker"
	"user-api/internal/cache"
	"user-api/internal/config"
	"user-api/internal/db"
	"user-api/internal/handler"
	"user-api/internal/metrics"
	"user-api/internal/middleware"
	"user-api/internal/outbox"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

func closeQuietly(c io.Closer, name string) {
	if err := c.Close(); err != nil {
		slog.Error("failed to close "+name, "error", err)
	}
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file, using environment variables")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	dbs, err := db.ConnectDB(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer closeQuietly(dbs, "database")

	redisCache := cache.NewRedisCache(cfg.RedisAddr)
	producer := broker.NewKafkaProducer(cfg.KafkaAddr)
	defer closeQuietly(producer, "kafka producer")

	storage := &db.UserStorage{DB: dbs, EventTopic: cfg.KafkaTopic}
	h := &handler.UserHandler{
		Storage: storage,
		Cache:   redisCache,
	}
	health := &handler.HealthHandler{
		DB:    storage,
		Cache: redisCache,
	}

	rateLimiter := middleware.NewRateLimiter(float64(cfg.RateLimitRPS), cfg.RateLimitBurst)

	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(middleware.RequestLogger(slog.Default()))
	r.Use(chimw.Recoverer)
	r.Use(metrics.Middleware)

	r.Get("/healthz", health.Healthz)
	r.Get("/readyz", health.Readyz)
	r.Method(http.MethodGet, "/metrics", metrics.Handler())

	if cfg.AuthEnabled {
		auth := &handler.AuthHandler{
			ClientID:     cfg.AuthClientID,
			ClientSecret: cfg.AuthClientSecret,
			JWTSecret:    []byte(cfg.JWTSecret),
			TokenTTL:     cfg.JWTTTL,
		}
		r.With(rateLimiter.Handler).Post("/auth/token", auth.Token)
	}

	r.Route("/users", func(r chi.Router) {
		r.Use(rateLimiter.Handler)
		r.Get("/", h.GetUsers)
		r.Get("/{id}", h.GetUserByID)

		// Mutating endpoints require a bearer token; reads stay open.
		r.Group(func(r chi.Router) {
			if cfg.AuthEnabled {
				jwtAuth := middleware.NewJWTAuth([]byte(cfg.JWTSecret), slog.Default())
				r.Use(jwtAuth.Handler)
			}
			r.Post("/", h.CreateUser)
			r.Put("/{id}", h.UpdateUser)
			r.Delete("/{id}", h.DeleteUser)
		})
	})

	if cfg.PprofEnabled {
		go func() {
			slog.Info("pprof listening", "addr", cfg.PprofAddr)
			if err := http.ListenAndServe(cfg.PprofAddr, nil); err != nil {
				slog.Error("pprof server stopped", "error", err)
			}
		}()
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go rateLimiter.CleanupLoop(ctx, time.Minute, 10*time.Minute)

	// The relay gets its own context so it keeps draining the outbox while
	// the HTTP server finishes in-flight requests, and is stopped last.
	relayCtx, relayCancel := context.WithCancel(context.Background())
	defer relayCancel()

	relay := outbox.NewRelay(
		&db.OutboxStorage{DB: dbs},
		&metrics.InstrumentedPublisher{Next: producer},
		cfg.OutboxPollInterval,
		cfg.OutboxBatchSize,
		slog.Default(),
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		relay.Run(relayCtx)
	}()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	relayCancel()
	wg.Wait()

	slog.Info("server stopped gracefully")

	return nil
}
