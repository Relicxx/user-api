// Package cache implements the Redis-backed cache used for cache-aside reads.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrCacheMiss is returned when the key is not present in the cache,
// so callers can distinguish a miss from a real cache failure.
var ErrCacheMiss = errors.New("cache miss")

// RedisCache is a thin wrapper over a Redis client.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache builds a cache client for the given address.
func NewRedisCache(addr string) *RedisCache {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	return &RedisCache{client: client}
}

// Set stores a value under key with the given TTL.
func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Get returns the value for key, or ErrCacheMiss if it is absent.
func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}

	return data, err
}

// Del removes key from the cache.
func (r *RedisCache) Del(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Ping reports whether Redis is reachable.
func (r *RedisCache) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
