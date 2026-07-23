package middleware

import (
	"context"
	"net/http"

	"feature-flag/internal/config"
	"feature-flag/internal/domain"

	"github.com/gin-gonic/gin"
)

func AuthMiddleware(clients []config.ClientConfig) gin.HandlerFunc {
	allowedKeys := make(map[string]config.ClientConfig)
	for _, client := range clients {
		allowedKeys[client.APIKey] = client
	}

	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-API-Key header"})
			c.Abort()
			return
		}

		client, ok := allowedKeys[apiKey]
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid X-API-Key"})
			c.Abort()
			return
		}

		c.Set(domain.CtxAPIKey, apiKey)
		c.Set(domain.CtxClientName, client.Name)
		c.Set(domain.CtxRateLimitCapacity, client.RateLimit.Capacity)
		c.Set(domain.CtxRateLimitRefill, client.RateLimit.RefillRate)

		// Propagate to standard context.Context for use case and database layers
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, domain.CtxAPIKey, apiKey)
		ctx = context.WithValue(ctx, domain.CtxClientName, client.Name)
		ctx = context.WithValue(ctx, domain.CtxRateLimitCapacity, client.RateLimit.Capacity)
		ctx = context.WithValue(ctx, domain.CtxRateLimitRefill, client.RateLimit.RefillRate)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
