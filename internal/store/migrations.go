package store

import "context"

const migrationSQL = `
CREATE TABLE IF NOT EXISTS events (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'general',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS telegram_users (
    id BIGSERIAL PRIMARY KEY,
    tg_chat_id BIGINT NOT NULL UNIQUE,
    tg_username TEXT NOT NULL DEFAULT '',
    link_code TEXT UNIQUE,
    link_code_expires_at TIMESTAMPTZ,
    linked BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id BIGSERIAL PRIMARY KEY,
    tg_user_id BIGINT NOT NULL REFERENCES telegram_users(id) ON DELETE CASCADE,
    event_id INT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    threshold_pct DOUBLE PRECISION NOT NULL DEFAULT 10,
    window_minutes INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tg_user_id, event_id)
);

-- Add columns if upgrading from older schema
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS threshold_pct DOUBLE PRECISION NOT NULL DEFAULT 10;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS window_minutes INT NOT NULL DEFAULT 1;

-- Rename old event names (idempotent — ignore if old names don't exist)
UPDATE events SET name = 'altura_drop', description = 'Alert when Altura metrics drop'
    WHERE name = 'altura_tvl_drop';

-- Seed events (idempotent)
INSERT INTO events (name, description, category) VALUES
    ('altura_drop', 'Alert when Altura metrics drop', 'altura'),
    ('altura_daily_report', 'Daily 8am UTC+8 report — Altura TVL, AVLT price, APR', 'altura'),
    ('neverland_drop', 'Alert when Neverland metrics drop', 'neverland'),
    ('neverland_daily_report', 'Daily 8am UTC+8 report — Neverland TVL, veDUST, DUST price, fees', 'neverland')
ON CONFLICT (name) DO NOTHING;

-- Update existing descriptions for HKT -> UTC+8 and configurable thresholds
UPDATE events SET description = 'Alert when Altura metrics drop' WHERE name = 'altura_drop';
UPDATE events SET description = 'Daily 8am UTC+8 report — Altura TVL, AVLT price, APR' WHERE name = 'altura_daily_report';
UPDATE events SET description = 'Alert when Neverland metrics drop' WHERE name = 'neverland_drop';
UPDATE events SET description = 'Daily 8am UTC+8 report — Neverland TVL, veDUST, DUST price, fees' WHERE name = 'neverland_daily_report';
`

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, migrationSQL)
	return err
}
