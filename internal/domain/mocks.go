package domain

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// MockDB is an in-memory implementation of the domain.DB interface for testing
type MockDB struct {
	mu    sync.RWMutex
	flags map[string]*Flag
}

func NewMockDB() *MockDB {
	return &MockDB{
		flags: make(map[string]*Flag),
	}
}

func (m *MockDB) CreateFlag(ctx context.Context, flag *Flag) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	compositeKey := fmt.Sprintf("%s:%s", flag.Key, flag.Environment)
	if _, exists := m.flags[compositeKey]; exists {
		return errors.New("flag already exists in this environment")
	}

	flag.ID = int64(len(m.flags) + 1)
	flag.CreatedAt = time.Now()
	flag.UpdatedAt = time.Now()
	
	// Store a copy
	copiedFlag := *flag
	m.flags[compositeKey] = &copiedFlag
	return nil
}

func (m *MockDB) GetFlag(ctx context.Context, key, env string) (*Flag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	compositeKey := fmt.Sprintf("%s:%s", key, env)
	flag, exists := m.flags[compositeKey]
	if !exists {
		return nil, ErrFlagNotFound
	}
	
	copiedFlag := *flag
	return &copiedFlag, nil
}

func (m *MockDB) UpdateFlag(ctx context.Context, flag *Flag) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	compositeKey := fmt.Sprintf("%s:%s", flag.Key, flag.Environment)
	existing, exists := m.flags[compositeKey]
	if !exists {
		return errors.New("flag not found to update")
	}

	existing.Enabled = flag.Enabled
	existing.RolloutPercentage = flag.RolloutPercentage
	existing.UpdatedAt = time.Now()

	*flag = *existing
	return nil
}

func (m *MockDB) DeleteFlag(ctx context.Context, key, env string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	compositeKey := fmt.Sprintf("%s:%s", key, env)
	if _, exists := m.flags[compositeKey]; !exists {
		return errors.New("flag not found to delete")
	}

	delete(m.flags, compositeKey)
	return nil
}

func (m *MockDB) ListFlags(ctx context.Context, env string) ([]*Flag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Flag, 0)
	for _, flag := range m.flags {
		if env == "" || flag.Environment == env {
			copiedFlag := *flag
			result = append(result, &copiedFlag)
		}
	}
	return result, nil
}

func (m *MockDB) Ping(ctx context.Context) error {
	return nil
}

func (m *MockDB) Close() {}

// MockCache is an in-memory implementation of the domain.Cache interface for testing
type MockCache struct {
	mu           sync.RWMutex
	store        map[string]string
	bucketTokens map[string]float64
	bucketRefill map[string]int64
}

func NewMockCache() *MockCache {
	return &MockCache{
		store:        make(map[string]string),
		bucketTokens: make(map[string]float64),
		bucketRefill: make(map[string]int64),
	}
}

func (m *MockCache) Get(ctx context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, exists := m.store[key]
	if !exists {
		return "", ErrCacheNotFound
	}
	return val, nil
}

func (m *MockCache) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
	return nil
}

func (m *MockCache) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, key)
	return nil
}

func (m *MockCache) EvaluateRateLimit(ctx context.Context, key string, capacity int, refillRate float64, nowUnix int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lastRefill, exists := m.bucketRefill[key]
	var tokens float64

	if !exists {
		tokens = float64(capacity)
		lastRefill = nowUnix
	} else {
		tokens = m.bucketTokens[key]
		elapsed := nowUnix - lastRefill
		if elapsed > 0 {
			tokens = tokens + (float64(elapsed) * refillRate)
			if tokens > float64(capacity) {
				tokens = float64(capacity)
			}
		}
	}

	if tokens >= 1.0 {
		tokens -= 1.0
		m.bucketTokens[key] = tokens
		m.bucketRefill[key] = nowUnix
		return true, nil
	}

	m.bucketTokens[key] = tokens
	m.bucketRefill[key] = nowUnix
	return false, nil
}

func (m *MockCache) Ping(ctx context.Context) error {
	return nil
}

func (m *MockCache) Close() error {
	return nil
}
