package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"

	"user-api/internal/broker"
	"user-api/internal/cache"
	"user-api/internal/config"
	"user-api/internal/db"
	"user-api/internal/handler"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment variables")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	dbs, err := db.ConnectDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
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

	go func() {
		log.Println("pprof listening on :6060")
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	log.Println("Server is running on port 8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
