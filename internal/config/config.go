package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL string
	RedisAddr   string
	KafkaAddr   string
	KafkaTopic  string

	HTTPAddr        string
	ShutdownTimeout time.Duration

	PprofEnabled bool
	PprofAddr    string
}

func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisAddr:       os.Getenv("REDIS_URL"),
		KafkaAddr:       os.Getenv("KAFKA_ADDR"),
		KafkaTopic:      getEnvDefault("KAFKA_TOPIC", "user-created"),
		HTTPAddr:        getEnvDefault("HTTP_ADDR", ":8080"),
		ShutdownTimeout: 10 * time.Second,
		PprofAddr:       getEnvDefault("PPROF_ADDR", "localhost:6060"),
	}

	if v := os.Getenv("PPROF_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PPROF_ENABLED value %q: %w", v, err)
		}
		cfg.PprofEnabled = enabled
	}

	required := map[string]string{
		"DATABASE_URL": cfg.DatabaseURL,
		"REDIS_URL":    cfg.RedisAddr,
		"KAFKA_ADDR":   cfg.KafkaAddr,
	}
	for name, value := range required {
		if value == "" {
			return nil, fmt.Errorf("required environment variable %s is not set", name)
		}
	}

	return cfg, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
