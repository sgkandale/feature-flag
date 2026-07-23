package flag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"time"

	"feature-flag/internal/domain"

	"golang.org/x/sync/singleflight"
)

type Usecase struct {
	db      domain.DB
	cache   domain.Cache
	sfGroup singleflight.Group
}

func NewUsecase(db domain.DB, cache domain.Cache) *Usecase {
	return &Usecase{
		db:    db,
		cache: cache,
	}
}

func (u *Usecase) CreateFlag(ctx context.Context, flag *domain.Flag) error {
	// Validate input
	if flag.Key == "" || flag.Environment == "" {
		return errors.New("flag key and environment are required")
	}
	if flag.RolloutPercentage < 0 || flag.RolloutPercentage > 100 {
		return errors.New("rollout percentage must be between 0 and 100")
	}

	// Set Owner based on authenticated client context
	clientName, _ := ctx.Value(domain.CtxClientName).(string)
	if clientName == "" {
		clientName = "admin" // Default fallback
	}
	flag.Owner = clientName

	// Create in DB
	err := u.db.CreateFlag(ctx, flag)
	if err != nil {
		return err
	}

	// Populate cache immediately (Write-Through)
	cacheKey := fmt.Sprintf("flag:%s:%s", flag.Environment, flag.Key)
	if data, err := json.Marshal(flag); err == nil {
		if cacheErr := u.cache.Set(ctx, cacheKey, string(data), 5*time.Minute); cacheErr != nil {
			log.Printf("[WARN] Failed to populate cache for key %s: %v", cacheKey, cacheErr)
		}
	} else {
		log.Printf("[ERROR] Failed to marshal flag for cache: %v", err)
	}

	return nil
}

func (u *Usecase) GetFlag(ctx context.Context, key, env string) (*domain.Flag, error) {
	if key == "" || env == "" {
		return nil, errors.New("flag key and environment are required")
	}

	cacheKey := fmt.Sprintf("flag:%s:%s", env, key)

	// 1. Check Cache first
	cachedVal, err := u.cache.Get(ctx, cacheKey)
	if err == nil {
		var flag domain.Flag
		if err := json.Unmarshal([]byte(cachedVal), &flag); err == nil {
			return &flag, nil
		}
	}

	// 2. Coalesce concurrent DB fetches using singleflight
	sfKey := fmt.Sprintf("sf:%s:%s", env, key)
	val, err, _ := u.sfGroup.Do(sfKey, func() (interface{}, error) {
		// Re-check cache inside singleflight to cover the race condition where a concurrent request
		// just updated the cache a microsecond ago
		cachedVal, err := u.cache.Get(ctx, cacheKey)
		if err == nil {
			var flag domain.Flag
			if err := json.Unmarshal([]byte(cachedVal), &flag); err == nil {
				return &flag, nil
			}
		}

		// Fetch from DB
		flag, err := u.db.GetFlag(ctx, key, env)
		if err != nil {
			return nil, err
		}

		// Save to Cache (TTL: 5 minutes)
		if data, err := json.Marshal(flag); err == nil {
			if cacheErr := u.cache.Set(ctx, cacheKey, string(data), 5*time.Minute); cacheErr != nil {
				log.Printf("[WARN] Failed to save flag to cache for key %s: %v", cacheKey, cacheErr)
			}
		} else {
			log.Printf("[ERROR] Failed to marshal flag for cache: %v", err)
		}

		return flag, nil
	})

	if err != nil {
		return nil, err
	}

	// Assert type
	flag, ok := val.(*domain.Flag)
	if !ok {
		return nil, errors.New("invalid type returned from singleflight")
	}

	return flag, nil
}

func (u *Usecase) UpdateFlag(ctx context.Context, input *domain.Flag) error {
	if input.Key == "" || input.Environment == "" {
		return errors.New("flag key and environment are required")
	}
	if input.RolloutPercentage < 0 || input.RolloutPercentage > 100 {
		return errors.New("rollout percentage must be between 0 and 100")
	}

	// Enforce Flag Ownership Authorization check
	existing, err := u.GetFlag(ctx, input.Key, input.Environment)
	if err != nil {
		return err
	}
	clientName, _ := ctx.Value(domain.CtxClientName).(string)
	if clientName != "" && existing.Owner != clientName && clientName != "admin-panel" && clientName != "default-admin" {
		return domain.ErrUnauthorizedOperation
	}

	// Keep existing owner
	input.Owner = existing.Owner

	// Update in DB
	err = u.db.UpdateFlag(ctx, input)
	if err != nil {
		return err
	}

	// Populate cache immediately (Write-Through)
	cacheKey := fmt.Sprintf("flag:%s:%s", input.Environment, input.Key)
	if data, err := json.Marshal(input); err == nil {
		if cacheErr := u.cache.Set(ctx, cacheKey, string(data), 5*time.Minute); cacheErr != nil {
			log.Printf("[WARN] Failed to update cache for key %s: %v", cacheKey, cacheErr)
		}
	} else {
		log.Printf("[ERROR] Failed to marshal flag for cache: %v", err)
	}

	return nil
}

func (u *Usecase) DeleteFlag(ctx context.Context, key, env string) error {
	if key == "" || env == "" {
		return errors.New("flag key and environment are required")
	}

	// Enforce Flag Ownership Authorization check
	existing, err := u.GetFlag(ctx, key, env)
	if err != nil {
		return err
	}
	clientName, _ := ctx.Value(domain.CtxClientName).(string)
	if clientName != "" && existing.Owner != clientName && clientName != "admin-panel" && clientName != "default-admin" {
		return domain.ErrUnauthorizedOperation
	}

	// Delete from DB
	err = u.db.DeleteFlag(ctx, key, env)
	if err != nil {
		return err
	}

	// Invalidate Cache
	if cacheErr := u.invalidateCache(ctx, key, env); cacheErr != nil {
		log.Printf("[WARN] Failed to invalidate cache for key %s in environment %s: %v", key, env, cacheErr)
	}

	return nil
}

func (u *Usecase) ListFlags(ctx context.Context, env string) ([]*domain.Flag, error) {
	return u.db.ListFlags(ctx, env)
}

func (u *Usecase) EvaluateFlag(ctx context.Context, key, env, callerID string) (bool, error) {
	// Retrieve Flag (using Cache-aside flow)
	flag, err := u.GetFlag(ctx, key, env)
	if err != nil {
		return false, err
	}

	// Evaluation Logic
	if !flag.Enabled {
		return false, nil
	}

	if flag.RolloutPercentage <= 0 {
		return false, nil
	}

	if flag.RolloutPercentage >= 100 {
		return true, nil
	}

	// Consistent Hashing
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s:%s:%s", flag.Key, flag.Environment, callerID)))
	hashVal := h.Sum32()

	// Modulo 100 gives a value between 0 and 99
	bucket := int(hashVal % 100)

	return bucket < flag.RolloutPercentage, nil
}

func (u *Usecase) invalidateCache(ctx context.Context, key, env string) error {
	cacheKey := fmt.Sprintf("flag:%s:%s", env, key)
	return u.cache.Delete(ctx, cacheKey)
}
