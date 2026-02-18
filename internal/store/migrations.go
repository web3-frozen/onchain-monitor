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
    direction TEXT NOT NULL DEFAULT 'drop',
    report_hour INT NOT NULL DEFAULT 8,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Add columns if upgrading from older schema
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS threshold_pct DOUBLE PRECISION NOT NULL DEFAULT 10;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS window_minutes INT NOT NULL DEFAULT 1;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT 'drop';
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS report_hour INT NOT NULL DEFAULT 8;

-- Drop unique constraint to allow multiple subscriptions per event with different configs
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_tg_user_id_event_id_key;

-- Rename old event names (idempotent)
UPDATE events SET name = 'altura_metric_alert', description = 'Alert when Altura metrics'
    WHERE name IN ('altura_tvl_drop', 'altura_drop');
UPDATE events SET name = 'neverland_metric_alert', description = 'Alert when Neverland metrics'
    WHERE name IN ('neverland_drop');

-- Seed events (idempotent)
INSERT INTO events (name, description, category) VALUES
    ('altura_metric_alert', 'Alert when Altura metrics', 'altura'),
    ('altura_daily_report', 'Daily UTC+8 report — Altura TVL, AVLT price, APR', 'altura'),
    ('neverland_metric_alert', 'Alert when Neverland metrics', 'neverland'),
    ('neverland_daily_report', 'Daily UTC+8 report — Neverland TVL, veDUST, DUST price, fees', 'neverland')
ON CONFLICT (name) DO NOTHING;

-- Update existing descriptions
UPDATE events SET description = 'Alert when Altura metrics' WHERE name = 'altura_metric_alert';
UPDATE events SET description = 'Daily UTC+8 report — Altura TVL, AVLT price, APR' WHERE name = 'altura_daily_report';
UPDATE events SET description = 'Alert when Neverland metrics' WHERE name = 'neverland_metric_alert';
UPDATE events SET description = 'Daily UTC+8 report — Neverland TVL, veDUST, DUST price, fees' WHERE name = 'neverland_daily_report';
`

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, migrationSQL)
	return err
}
