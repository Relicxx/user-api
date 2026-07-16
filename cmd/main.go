package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"user-api/internal/broker"
	"user-api/internal/cache"
	"user-api/internal/config"
	"user-api/internal/db"
	"user-api/internal/handler"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Printf("server exited with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment variables")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	dbs, err := db.ConnectDB(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer dbs.Close()

	redisCache := cache.NewRedisCache(cfg.RedisAddr)
	producer := broker.NewKafkaProducer(cfg.KafkaAddr, cfg.KafkaTopic)
	defer producer.Close()

	storage := &db.UserStorage{DB: dbs}
	h := &handler.UserHandler{
		Storage:  storage,
		Cache:    redisCache,
		Producer: producer,
	}
	health := &handler.HealthHandler{
		DB:    storage,
		Cache: redisCache,
	}

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", health.Healthz)
	r.Get("/readyz", health.Readyz)

	r.Route("/users", func(r chi.Router) {
		r.Get("/", h.GetUsers)
		r.Get("/{id}", h.GetUserByID)
		r.Post("/", h.CreateUser)
		r.Put("/{id}", h.UpdateUser)
		r.Delete("/{id}", h.DeleteUser)
	})

	if cfg.PprofEnabled {
		go func() {
			log.Printf("pprof listening on %s", cfg.PprofAddr)
			if err := http.ListenAndServe(cfg.PprofAddr, nil); err != nil {
				log.Printf("pprof server stopped: %v", err)
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

	errCh := make(chan error, 1)
	go func() {
		log.Printf("server listening on %s", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Println("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	log.Println("server stopped gracefully")

	return nil
}
