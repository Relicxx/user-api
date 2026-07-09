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

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(addr string) *RedisCache {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	return &RedisCache{client: client}
}

func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}

	return data, err
}

func (r *RedisCache) Del(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}
