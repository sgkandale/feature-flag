package domain

import "errors"

// Package-level error constants for consistent error comparison
var (
	ErrFlagNotFound          = errors.New("flag not found")
	ErrCacheNotFound         = errors.New("cache key not found")
	ErrUnauthorizedOperation = errors.New("unauthorized: you do not own this flag")
)
