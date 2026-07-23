package domain

import (
	"time"
)

type Flag struct {
	ID                int64     `json:"id"`
	Key               string    `json:"key"`
	Environment       string    `json:"environment"`
	Enabled           bool      `json:"enabled"`
	RolloutPercentage int       `json:"rollout_percentage"`
	Owner             string    `json:"owner"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
