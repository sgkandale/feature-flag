package middleware

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

type RequestLog struct {
	Timestamp string `json:"timestamp"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	Latency   string `json:"latency"`
	ClientIP  string `json:"client_ip"`
	Error     string `json:"error,omitempty"`
}

// LoggerMiddleware logs request details in structured JSON format
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)

		errStr := ""
		if len(c.Errors) > 0 {
			errStr = c.Errors.String()
		}

		logEntry := RequestLog{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Method:    c.Request.Method,
			Path:      c.Request.URL.Path,
			Status:    c.Writer.Status(),
			Latency:   latency.String(),
			ClientIP:  c.ClientIP(),
			Error:     errStr,
		}

		data, err := json.Marshal(logEntry)
		if err == nil {
			log.Println(string(data))
		} else {
			log.Printf("[ERROR] Failed to marshal log: %v", err)
		}
	}
}
