package flag

import (
	"errors"
	"time"

	"feature-flag/internal/domain"
)

type CreateFlagRequest struct {
	Key               string `json:"key"`
	Environment       string `json:"environment"`
	Enabled           bool   `json:"enabled"`
	RolloutPercentage int    `json:"rollout_percentage"`
}

func (req *CreateFlagRequest) Validate() error {
	if req.Key == "" {
		return errors.New("key is required")
	}
	if req.Environment == "" {
		return errors.New("environment is required")
	}
	if req.RolloutPercentage < 0 || req.RolloutPercentage > 100 {
		return errors.New("rollout percentage must be between 0 and 100")
	}
	return nil
}

func (req *CreateFlagRequest) ToDomain() *domain.Flag {
	return &domain.Flag{
		Key:               req.Key,
		Environment:       req.Environment,
		Enabled:           req.Enabled,
		RolloutPercentage: req.RolloutPercentage,
	}
}

type UpdateFlagRequest struct {
	Environment       string `json:"environment"`
	Enabled           bool   `json:"enabled"`
	RolloutPercentage int    `json:"rollout_percentage"`
}

func (req *UpdateFlagRequest) Validate() error {
	if req.Environment == "" {
		return errors.New("environment is required")
	}
	if req.RolloutPercentage < 0 || req.RolloutPercentage > 100 {
		return errors.New("rollout percentage must be between 0 and 100")
	}
	return nil
}

func (req *UpdateFlagRequest) ToDomain(key string) *domain.Flag {
	return &domain.Flag{
		Key:               key,
		Environment:       req.Environment,
		Enabled:           req.Enabled,
		RolloutPercentage: req.RolloutPercentage,
	}
}

type FlagResponse struct {
	ID                int64     `json:"id"`
	Key               string    `json:"key"`
	Environment       string    `json:"environment"`
	Enabled           bool      `json:"enabled"`
	RolloutPercentage int       `json:"rollout_percentage"`
	Owner             string    `json:"owner"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func NewFlagResponse(f *domain.Flag) *FlagResponse {
	if f == nil {
		return nil
	}
	return &FlagResponse{
		ID:                f.ID,
		Key:               f.Key,
		Environment:       f.Environment,
		Enabled:           f.Enabled,
		RolloutPercentage: f.RolloutPercentage,
		Owner:             f.Owner,
		CreatedAt:         f.CreatedAt,
		UpdatedAt:         f.UpdatedAt,
	}
}

func NewFlagListResponse(flags []*domain.Flag) []*FlagResponse {
	res := make([]*FlagResponse, len(flags))
	for i, f := range flags {
		res[i] = NewFlagResponse(f)
	}
	return res
}
