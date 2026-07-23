package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"feature-flag/internal/config"
	"feature-flag/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDB struct {
	pool *pgxpool.Pool
}

// NewPostgresDB creates a new pgxpool.Pool database connection pool based on configuration
func NewPostgresDB(ctx context.Context, cfg config.PostgresConfig) (*PostgresDB, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName, cfg.SSLMode)

	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("unable to parse connection string: %w", err)
	}

	// Apply pooling settings
	poolConfig.MaxConns = int32(cfg.MaxConns)
	poolConfig.MinConns = int32(cfg.MinConns)
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// Ping connection to verify it's working
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	return &PostgresDB{pool: pool}, nil
}

// Close closes the connection pool
func (db *PostgresDB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// CreateFlag inserts a new flag record in the database
func (db *PostgresDB) CreateFlag(ctx context.Context, flag *domain.Flag) error {
	query := `
		INSERT INTO flags (key, environment, enabled, rollout_percentage, owner, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`
	err := db.pool.QueryRow(ctx, query, flag.Key, flag.Environment, flag.Enabled, flag.RolloutPercentage, flag.Owner).
		Scan(&flag.ID, &flag.CreatedAt, &flag.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create flag: %w", err)
	}
	return nil
}

// GetFlag retrieves a single flag by key and environment
func (db *PostgresDB) GetFlag(ctx context.Context, key, env string) (*domain.Flag, error) {
	query := `
		SELECT id, key, environment, enabled, rollout_percentage, owner, created_at, updated_at
		FROM flags
		WHERE key = $1 AND environment = $2
	`
	flag := &domain.Flag{}
	err := db.pool.QueryRow(ctx, query, key, env).
		Scan(&flag.ID, &flag.Key, &flag.Environment, &flag.Enabled, &flag.RolloutPercentage, &flag.Owner, &flag.CreatedAt, &flag.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrFlagNotFound
		}
		return nil, fmt.Errorf("failed to get flag: %w", err)
	}
	return flag, nil
}

// UpdateFlag updates an existing flag
func (db *PostgresDB) UpdateFlag(ctx context.Context, flag *domain.Flag) error {
	query := `
		UPDATE flags
		SET enabled = $1, rollout_percentage = $2, updated_at = NOW()
		WHERE key = $3 AND environment = $4
		RETURNING id, created_at, updated_at
	`
	err := db.pool.QueryRow(ctx, query, flag.Enabled, flag.RolloutPercentage, flag.Key, flag.Environment).
		Scan(&flag.ID, &flag.CreatedAt, &flag.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("flag not found to update")
		}
		return fmt.Errorf("failed to update flag: %w", err)
	}
	return nil
}

// DeleteFlag deletes a flag by key and environment
func (db *PostgresDB) DeleteFlag(ctx context.Context, key, env string) error {
	query := `
		DELETE FROM flags
		WHERE key = $1 AND environment = $2
	`
	result, err := db.pool.Exec(ctx, query, key, env)
	if err != nil {
		return fmt.Errorf("failed to delete flag: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("flag not found to delete")
	}
	return nil
}

// ListFlags retrieves all flags, optionally filtered by environment
func (db *PostgresDB) ListFlags(ctx context.Context, env string) ([]*domain.Flag, error) {
	var query string
	var args []any

	if env != "" {
		query = `
			SELECT id, key, environment, enabled, rollout_percentage, owner, created_at, updated_at
			FROM flags
			WHERE environment = $1
			ORDER BY created_at DESC
		`
		args = append(args, env)
	} else {
		query = `
			SELECT id, key, environment, enabled, rollout_percentage, owner, created_at, updated_at
			FROM flags
			ORDER BY created_at DESC
		`
	}

	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list flags: %w", err)
	}
	defer rows.Close()

	flags := make([]*domain.Flag, 0)
	for rows.Next() {
		flag := &domain.Flag{}
		err := rows.Scan(&flag.ID, &flag.Key, &flag.Environment, &flag.Enabled, &flag.RolloutPercentage, &flag.Owner, &flag.CreatedAt, &flag.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan flag row: %w", err)
		}
		flags = append(flags, flag)
	}

	return flags, nil
}

// Ping verifies PostgreSQL connection pool health
func (db *PostgresDB) Ping(ctx context.Context) error {
	return db.pool.Ping(ctx)
}
