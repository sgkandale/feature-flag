package database

import (
	"context"
	"fmt"
	"time"

	"feature-flag/internal/config"
	"feature-flag/internal/domain"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
}

// Token Bucket Lua Script for Atomic Rate Limiting
const rateLimitLuaScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

-- HMGET returns array of string or bulk values
local rate_limit = redis.call('HMGET', key, 'tokens', 'last_refilled_at')
local tokens = tonumber(rate_limit[1])
local last_refilled_at = tonumber(rate_limit[2])

if not tokens then
    -- First time: initialize bucket
    tokens = capacity
    last_refilled_at = now
else
    -- Add refilled tokens based on time elapsed
    local elapsed = now - last_refilled_at
    if elapsed > 0 then
        tokens = math.min(capacity, tokens + (elapsed * refill_rate))
    end
end

-- Check if we have at least 1 token to consume
if tokens >= 1 then
    tokens = tokens - 1
    redis.call('HMSET', key, 'tokens', tokens, 'last_refilled_at', now)
    redis.call('EXPIRE', key, 86400) -- Expire after 24 hours of inactivity
    return 1 -- Request Allowed
else
    return 0 -- Request Rate Limited
end
`

var redisScript = redis.NewScript(rateLimitLuaScript)

// NewRedisCache creates a new Redis client wrapper implementing domain.Cache
func NewRedisCache(cfg config.RedisConfig) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Check if client can connect
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisCache{client: client}, nil
}

// Get gets a value from cache
func (r *RedisCache) Get(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", domain.ErrCacheNotFound
		}
		return "", fmt.Errorf("redis GET error: %w", err)
	}
	return val, nil
}

// Set sets a value in cache with expiration
func (r *RedisCache) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	err := r.client.Set(ctx, key, value, expiration).Err()
	if err != nil {
		return fmt.Errorf("redis SET error: %w", err)
	}
	return nil
}

// Delete removes a value from cache
func (r *RedisCache) Delete(ctx context.Context, key string) error {
	err := r.client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("redis DEL error: %w", err)
	}
	return nil
}

// EvaluateRateLimit runs the atomic Token Bucket Lua script
func (r *RedisCache) EvaluateRateLimit(ctx context.Context, key string, capacity int, refillRate float64, nowUnix int64) (bool, error) {
	// Keys: [rate_limit_key]
	// Args: [capacity, refill_rate, nowUnix]
	result, err := redisScript.Run(ctx, r.client, []string{key}, capacity, refillRate, nowUnix).Int()
	if err != nil {
		return false, fmt.Errorf("failed to run rate limiter lua script: %w", err)
	}

	return result == 1, nil
}

// Ping verifies Redis connectivity health
func (r *RedisCache) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close closes the Redis client connection
func (r *RedisCache) Close() error {
	return r.client.Close()
}
