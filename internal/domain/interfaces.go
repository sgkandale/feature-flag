package domain

import (
	"context"
	"time"
)

// DB defines the contract for our relational database storage (PostgreSQL)
type DB interface {
	CreateFlag(ctx context.Context, flag *Flag) error
	GetFlag(ctx context.Context, key, env string) (*Flag, error)
	UpdateFlag(ctx context.Context, flag *Flag) error
	DeleteFlag(ctx context.Context, key, env string) error
	ListFlags(ctx context.Context, env string) ([]*Flag, error)
	Ping(ctx context.Context) error
	Close()
}

// Cache defines the contract for our memory storage / cache layer (Redis)
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, expiration time.Duration) error
	Delete(ctx context.Context, key string) error
	// EvaluateRateLimit runs the token bucket algorithm atomically and returns if request is allowed
	EvaluateRateLimit(ctx context.Context, key string, capacity int, refillRate float64, nowUnix int64) (bool, error)
	Ping(ctx context.Context) error
	Close() error
}
