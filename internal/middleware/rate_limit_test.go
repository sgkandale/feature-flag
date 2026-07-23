package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"feature-flag/internal/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Configure mock cache
	cache := domain.NewMockCache()
	capacity := 3
	refillRate := 1.0 // 1 token per second

	// Setup router
	router := gin.New()
	
	// Create middleware chain
	auth := func(c *gin.Context) {
		c.Set(domain.CtxAPIKey, "test-api-key")
		c.Set(domain.CtxRateLimitCapacity, capacity)
		c.Set(domain.CtxRateLimitRefill, refillRate)
		c.Next()
	}
	rateLimiter := RateLimitMiddleware(cache)

	router.GET("/test", auth, rateLimiter, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Make 3 requests immediately (should be allowed)
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i+1)
	}

	// 4th request immediately (should be rate limited)
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/test", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "4th request should be rate limited")

	// Wait 1 second for bucket to refill
	// MockCache uses the nowUnix argument to calculate elapsed time, so we simulate this by passing future unix times.
	// However, our middleware uses time.Now().Unix(), so let's actually sleep 1 second in our real clock.
	time.Sleep(1 * time.Second)

	// 5th request (should be allowed now due to refill)
	w = httptest.NewRecorder()
	req, _ = http.NewRequestWithContext(context.Background(), "GET", "/test", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "5th request should be allowed after waiting 1s for refill")
}
