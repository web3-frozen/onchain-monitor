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

-- Seed default events (idempotent)
INSERT INTO events (name, description, category) VALUES
    ('altura_tvl_drop', 'Alert when Altura TVL drops more than 10% within 1 minute', 'altura'),
    ('altura_daily_report', 'Daily 8am HKT report with TVL, AVLT price, and APR', 'altura')
ON CONFLICT (name) DO NOTHING;
`

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, migrationSQL)
	return err
}
