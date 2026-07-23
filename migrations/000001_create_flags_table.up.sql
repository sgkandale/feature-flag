CREATE TABLE IF NOT EXISTS flags (
    id BIGSERIAL PRIMARY KEY,
    key VARCHAR(255) NOT NULL,
    environment VARCHAR(50) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    rollout_percentage INT NOT NULL DEFAULT 0,
    owner VARCHAR(100) NOT NULL DEFAULT 'admin',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_flags_key_env UNIQUE (key, environment),
    CONSTRAINT chk_rollout CHECK (rollout_percentage >= 0 AND rollout_percentage <= 100)
);

CREATE INDEX IF NOT EXISTS idx_flags_key_env ON flags (key, environment);
