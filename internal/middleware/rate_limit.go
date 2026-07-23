package middleware

import (
	"fmt"
	"net/http"
	"time"

	"feature-flag/internal/domain"

	"github.com/gin-gonic/gin"
)

// RateLimitMiddleware enforces token-bucket rate limits per API key using client-specific rates
func RateLimitMiddleware(cache domain.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey, exists := c.Get(domain.CtxAPIKey)
		if !exists {
			// Fallback to Client IP if request isn't authenticated (preventing raw DDoS)
			apiKey = c.ClientIP()
		}

		// Default fallback values if client-specific rate limits are not present in request context
		capacity := 10
		refillRate := 2.0

		// Extract dynamic client rate limits populated by AuthMiddleware
		if capVal, ok := c.Get(domain.CtxRateLimitCapacity); ok {
			if val, ok := capVal.(int); ok {
				capacity = val
			}
		}
		if refillVal, ok := c.Get(domain.CtxRateLimitRefill); ok {
			if val, ok := refillVal.(float64); ok {
				refillRate = val
			}
		}

		key := fmt.Sprintf("rate_limit:%v", apiKey)
		now := time.Now().Unix()

		allowed, err := cache.EvaluateRateLimit(c.Request.Context(), key, capacity, refillRate, now)
		if err != nil {
			// Fail-open strategy to ensure service availability if Redis/Cache is down, but log the error
			_ = c.Error(fmt.Errorf("rate limiter cache error: %w", err))
			c.Next()
			return
		}

		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
