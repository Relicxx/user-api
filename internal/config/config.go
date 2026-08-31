// Package config loads and validates service configuration from the
// environment, failing fast on missing or malformed values.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime settings of the service.
type Config struct {
	DatabaseURL string
	RedisAddr   string
	KafkaAddr   string
	KafkaTopic  string

	HTTPAddr        string
	ShutdownTimeout time.Duration

	OutboxPollInterval time.Duration
	OutboxBatchSize    int

	RateLimitRPS   int
	RateLimitBurst int

	AuthEnabled      bool
	AuthClientID     string
	AuthClientSecret string
	JWTSecret        string
	JWTTTL           time.Duration

	PprofEnabled bool
	PprofAddr    string
}

// Load reads the configuration from environment variables and validates it.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		RedisAddr:        os.Getenv("REDIS_URL"),
		KafkaAddr:        os.Getenv("KAFKA_ADDR"),
		KafkaTopic:       getEnvDefault("KAFKA_TOPIC", "user-created"),
		HTTPAddr:         getEnvDefault("HTTP_ADDR", ":8080"),
		ShutdownTimeout:  10 * time.Second,
		AuthEnabled:      true,
		AuthClientID:     os.Getenv("AUTH_CLIENT_ID"),
		AuthClientSecret: os.Getenv("AUTH_CLIENT_SECRET"),
		JWTSecret:        os.Getenv("JWT_SECRET"),
		PprofAddr:        getEnvDefault("PPROF_ADDR", "localhost:6060"),
	}

	var err error
	if cfg.OutboxPollInterval, err = getEnvDuration("OUTBOX_POLL_INTERVAL", time.Second); err != nil {
		return nil, err
	}
	if cfg.OutboxBatchSize, err = getEnvInt("OUTBOX_BATCH_SIZE", 100); err != nil {
		return nil, err
	}
	if cfg.RateLimitRPS, err = getEnvInt("RATE_LIMIT_RPS", 20); err != nil {
		return nil, err
	}
	if cfg.RateLimitBurst, err = getEnvInt("RATE_LIMIT_BURST", 40); err != nil {
		return nil, err
	}
	if cfg.JWTTTL, err = getEnvDuration("JWT_TTL", 15*time.Minute); err != nil {
		return nil, err
	}

	// Auth is on by default so mutating endpoints are never left open by
	// accident; disabling it is an explicit opt-out.
	if v := os.Getenv("AUTH_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid AUTH_ENABLED value %q: %w", v, err)
		}
		cfg.AuthEnabled = enabled
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
	if cfg.AuthEnabled {
		required["AUTH_CLIENT_ID"] = cfg.AuthClientID
		required["AUTH_CLIENT_SECRET"] = cfg.AuthClientSecret
		required["JWT_SECRET"] = cfg.JWTSecret
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

func getEnvDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid %s value %q: must be a positive duration", key, v)
	}
	return d, nil
}

func getEnvInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid %s value %q: must be a positive integer", key, v)
	}
	return n, nil
}
