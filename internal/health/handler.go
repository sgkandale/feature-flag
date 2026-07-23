package health

import (
	"context"
	"net/http"
	"sync"
	"time"

	"feature-flag/internal/domain"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	db    domain.DB
	cache domain.Cache
}

func NewHandler(db domain.DB, cache domain.Cache) *Handler {
	return &Handler{
		db:    db,
		cache: cache,
	}
}

// HealthCheck verifies connectivity of PostgreSQL and Redis concurrently
func (h *Handler) HealthCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	var dbErr, cacheErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		dbErr = h.db.Ping(ctx)
	}()

	go func() {
		defer wg.Done()
		cacheErr = h.cache.Ping(ctx)
	}()

	wg.Wait()

	status := "healthy"
	postgresStatus := "up"
	redisStatus := "up"
	statusCode := http.StatusOK

	if dbErr != nil {
		status = "unhealthy"
		postgresStatus = "down"
		statusCode = http.StatusServiceUnavailable
	}

	if cacheErr != nil {
		status = "unhealthy"
		redisStatus = "down"
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, gin.H{
		"status":   status,
		"postgres": postgresStatus,
		"redis":    redisStatus,
	})
}
