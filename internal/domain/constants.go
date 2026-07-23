package domain

// Context Key Constants used across middlewares and handlers to prevent string literal duplication
const (
	CtxAPIKey            = "api_key"
	CtxClientName        = "client_name"
	CtxRateLimitCapacity = "rate_limit_capacity"
	CtxRateLimitRefill   = "rate_limit_refill_rate"
)
