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
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tg_user_id, event_id)
);

-- Rename old event names (idempotent — ignore if old names don't exist)
UPDATE events SET name = 'altura_drop', description = 'Alert when Altura metrics drop >10% in 1 minute'
    WHERE name = 'altura_tvl_drop';

-- Seed events (idempotent)
INSERT INTO events (name, description, category) VALUES
    ('altura_drop', 'Alert when Altura metrics drop >10% in 1 minute', 'altura'),
    ('altura_daily_report', 'Daily 8am HKT report — Altura TVL, AVLT price, APR', 'altura'),
    ('neverland_drop', 'Alert when Neverland metrics drop >10% in 1 minute', 'neverland'),
    ('neverland_daily_report', 'Daily 8am HKT report — Neverland TVL, veDUST, DUST price, fees', 'neverland')
ON CONFLICT (name) DO NOTHING;
`

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, migrationSQL)
	return err
}
