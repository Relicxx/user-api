package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"user-api/internal/broker"
	"user-api/internal/cache"
	"user-api/internal/db"
	"user-api/internal/handler"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment variables")
	}
	dbs, err := db.ConnectDB()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbs.Close()

	redisCache := cache.NewRedisCache(os.Getenv("REDIS_URL"))
	producer := broker.NewKafkaProducer(os.Getenv("KAFKA_ADDR"), "user-created")
	defer producer.Close()

	storage := &db.UserStorage{DB: dbs}
	h := &handler.UserHandler{
		Storage:  storage,
		Cache:    redisCache,
		Producer: producer,
	}

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

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
