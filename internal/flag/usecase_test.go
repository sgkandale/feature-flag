package flag

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"feature-flag/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestUsecase_CreateFlag(t *testing.T) {
	db := domain.NewMockDB()
	cache := domain.NewMockCache()
	uc := NewUsecase(db, cache)
	ctx := context.Background()

	// 1. Success case
	f := &domain.Flag{
		Key:               "test-flag",
		Environment:       "dev",
		Enabled:           true,
		RolloutPercentage: 50,
	}
	err := uc.CreateFlag(ctx, f)
	assert.NoError(t, err)
	assert.NotZero(t, f.ID)

	// 2. Duplicate key+env error
	err = uc.CreateFlag(ctx, f)
	assert.Error(t, err)

	// 3. Validation error: RolloutPercentage > 100
	invalidF1 := &domain.Flag{
		Key:               "invalid-flag-1",
		Environment:       "dev",
		Enabled:           true,
		RolloutPercentage: 150,
	}
	err = uc.CreateFlag(ctx, invalidF1)
	assert.Error(t, err)

	// 4. Validation error: Empty Key
	invalidF2 := &domain.Flag{
		Key:               "",
		Environment:       "dev",
		Enabled:           true,
		RolloutPercentage: 50,
	}
	err = uc.CreateFlag(ctx, invalidF2)
	assert.Error(t, err)
}

func TestUsecase_GetFlag_CacheAside(t *testing.T) {
	db := domain.NewMockDB()
	cache := domain.NewMockCache()
	uc := NewUsecase(db, cache)
	ctx := context.Background()

	f := &domain.Flag{
		Key:               "cached-flag",
		Environment:       "prod",
		Enabled:           true,
		RolloutPercentage: 100,
	}
	_ = uc.CreateFlag(ctx, f)

	// Invalidate cache to test the GetFlag cache-aside fetch path
	cacheKey := "flag:prod:cached-flag"
	_ = uc.invalidateCache(ctx, "cached-flag", "prod")
	
	val, err := cache.Get(ctx, cacheKey)
	assert.ErrorIs(t, err, domain.ErrCacheNotFound)
	assert.Empty(t, val)

	// GetFlag (Cache Miss -> DB Fetch -> Cache Populate)
	fetched, err := uc.GetFlag(ctx, "cached-flag", "prod")
	assert.NoError(t, err)
	assert.Equal(t, f.Key, fetched.Key)

	// Verify Cache is now populated
	val, err = cache.Get(ctx, cacheKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, val)

	// Modify DB directly to see if Cache-Aside works (it should return cached value, not DB)
	db.DeleteFlag(ctx, "cached-flag", "prod")
	fetchedCached, err := uc.GetFlag(ctx, "cached-flag", "prod")
	assert.NoError(t, err) // No error because it reads from Cache!
	assert.Equal(t, f.Key, fetchedCached.Key)

	// Invalidate Cache and test GetFlag again (should fail because DB has deleted it)
	_ = uc.invalidateCache(ctx, "cached-flag", "prod")
	_, err = uc.GetFlag(ctx, "cached-flag", "prod")
	assert.Error(t, err) // Now it fails
}

func TestUsecase_EvaluateFlag_ConsistentHashing(t *testing.T) {
	db := domain.NewMockDB()
	cache := domain.NewMockCache()
	uc := NewUsecase(db, cache)
	ctx := context.Background()

	// 1. Setup flags
	// Flag 1: Disabled
	_ = uc.CreateFlag(ctx, &domain.Flag{Key: "disabled-flag", Environment: "prod", Enabled: false, RolloutPercentage: 100})
	// Flag 2: Enabled, Rollout 0
	_ = uc.CreateFlag(ctx, &domain.Flag{Key: "rollout-0", Environment: "prod", Enabled: true, RolloutPercentage: 0})
	// Flag 3: Enabled, Rollout 100
	_ = uc.CreateFlag(ctx, &domain.Flag{Key: "rollout-100", Environment: "prod", Enabled: true, RolloutPercentage: 100})
	// Flag 4: Enabled, Rollout 30
	_ = uc.CreateFlag(ctx, &domain.Flag{Key: "rollout-30", Environment: "prod", Enabled: true, RolloutPercentage: 30})

	// Evaluate Disabled Flag
	val, err := uc.EvaluateFlag(ctx, "disabled-flag", "prod", "user1")
	assert.NoError(t, err)
	assert.False(t, val)

	// Evaluate Rollout 0
	val, err = uc.EvaluateFlag(ctx, "rollout-0", "prod", "user1")
	assert.NoError(t, err)
	assert.False(t, val)

	// Evaluate Rollout 100
	val, err = uc.EvaluateFlag(ctx, "rollout-100", "prod", "user1")
	assert.NoError(t, err)
	assert.True(t, val)

	// Evaluate Rollout 30 with consistent user evaluations
	evals1 := make(map[string]bool)
	for i := 0; i < 10; i++ {
		callerID := fmt.Sprintf("user_%d", i)
		res1, _ := uc.EvaluateFlag(ctx, "rollout-30", "prod", callerID)
		res2, _ := uc.EvaluateFlag(ctx, "rollout-30", "prod", callerID)
		assert.Equal(t, res1, res2, "Evaluation must be consistent for the same caller")
		evals1[callerID] = res1
	}

	// Verify rollout behavior distribution: out of 100 users, approximately 30 should get true
	trueCount := 0
	for i := 0; i < 1000; i++ {
		callerID := fmt.Sprintf("user_%d", i)
		res, _ := uc.EvaluateFlag(ctx, "rollout-30", "prod", callerID)
		if res {
			trueCount++
		}
	}
	// We expect roughly 300 out of 1000 to be true (say, between 250 and 350)
	assert.GreaterOrEqual(t, trueCount, 250)
	assert.LessOrEqual(t, trueCount, 350)
	t.Logf("Consistent rollout percentage test: %d%% users evaluated to true (Target: 30%%)", trueCount/10)
}

type CounterDB struct {
	*domain.MockDB
	Calls int
	mu    sync.Mutex
}

func (c *CounterDB) GetFlag(ctx context.Context, key, env string) (*domain.Flag, error) {
	c.mu.Lock()
	c.Calls++
	c.mu.Unlock()

	// Simulate light latency so concurrent calls overlap in flight
	time.Sleep(10 * time.Millisecond)

	return c.MockDB.GetFlag(ctx, key, env)
}

func TestUsecase_GetFlag_Singleflight(t *testing.T) {
	dbMock := domain.NewMockDB()
	counterDB := &CounterDB{MockDB: dbMock}
	cache := domain.NewMockCache()
	uc := NewUsecase(counterDB, cache)
	ctx := context.Background()

	// Create a flag in the DB
	f := &domain.Flag{
		Key:               "sf-flag",
		Environment:       "prod",
		Enabled:           true,
		RolloutPercentage: 100,
	}
	_ = uc.CreateFlag(ctx, f)

	// Invalidate cache so we get a cache miss
	_ = uc.invalidateCache(ctx, "sf-flag", "prod")

	// Fire 10 concurrent requests to GetFlag simultaneously
	var wg sync.WaitGroup
	numRequests := 10
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			res, err := uc.GetFlag(ctx, "sf-flag", "prod")
			assert.NoError(t, err)
			assert.Equal(t, "sf-flag", res.Key)
		}()
	}

	wg.Wait()

	// Singleflight must ensure only 1 DB call was made across all 10 concurrent requests
	assert.Equal(t, 1, counterDB.Calls, "Expected exactly 1 database lookup under singleflight")
}
