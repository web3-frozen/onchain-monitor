package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// --- Events ---

type Event struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) ListEvents(ctx context.Context) ([]Event, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, description, category, enabled, created_at FROM events WHERE enabled = true ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.Category, &e.Enabled, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Telegram Users ---

type TelegramUser struct {
	ID                int64     `json:"id"`
	TgChatID          int64     `json:"tg_chat_id"`
	TgUsername        string    `json:"tg_username"`
	LinkCode          string    `json:"link_code,omitempty"`
	LinkCodeExpiresAt time.Time `json:"link_code_expires_at,omitempty"`
	Linked            bool      `json:"linked"`
	CreatedAt         time.Time `json:"created_at"`
}

func (s *Store) UpsertTelegramUser(ctx context.Context, chatID int64, username, linkCode string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO telegram_users (tg_chat_id, tg_username, link_code, link_code_expires_at, linked)
		VALUES ($1, $2, $3, $4, false)
		ON CONFLICT (tg_chat_id) DO UPDATE
			SET link_code = $3, link_code_expires_at = $4, tg_username = $2`,
		chatID, username, linkCode, expiresAt)
	return err
}

func (s *Store) LinkByCode(ctx context.Context, code string) (*TelegramUser, error) {
	var u TelegramUser
	err := s.pool.QueryRow(ctx, `
		UPDATE telegram_users SET linked = true, link_code = NULL, link_code_expires_at = NULL
		WHERE link_code = $1 AND link_code_expires_at > now()
		RETURNING id, tg_chat_id, tg_username, linked, created_at`, code).
		Scan(&u.ID, &u.TgChatID, &u.TgUsername, &u.Linked, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetTelegramUser(ctx context.Context, chatID int64) (*TelegramUser, error) {
	var u TelegramUser
	err := s.pool.QueryRow(ctx, `
		SELECT id, tg_chat_id, tg_username, linked, created_at
		FROM telegram_users WHERE tg_chat_id = $1`, chatID).
		Scan(&u.ID, &u.TgChatID, &u.TgUsername, &u.Linked, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// --- Subscriptions ---

type Subscription struct {
	ID        int64     `json:"id"`
	TgUserID  int64     `json:"tg_user_id"`
	EventID   int       `json:"event_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListSubscriptions(ctx context.Context, tgChatID int64) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.tg_user_id, s.event_id, s.created_at
		FROM subscriptions s
		JOIN telegram_users u ON u.id = s.tg_user_id
		WHERE u.tg_chat_id = $1
		ORDER BY s.id`, tgChatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.TgUserID, &sub.EventID, &sub.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *Store) Subscribe(ctx context.Context, tgChatID int64, eventID int) (*Subscription, error) {
	var sub Subscription
	err := s.pool.QueryRow(ctx, `
		INSERT INTO subscriptions (tg_user_id, event_id)
		SELECT u.id, $2 FROM telegram_users u WHERE u.tg_chat_id = $1
		ON CONFLICT (tg_user_id, event_id) DO UPDATE SET created_at = subscriptions.created_at
		RETURNING id, tg_user_id, event_id, created_at`, tgChatID, eventID).
		Scan(&sub.ID, &sub.TgUserID, &sub.EventID, &sub.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *Store) Unsubscribe(ctx context.Context, subID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM subscriptions WHERE id = $1`, subID)
	return err
}

func (s *Store) GetSubscriberChatIDs(ctx context.Context, eventName string) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.tg_chat_id
		FROM subscriptions s
		JOIN telegram_users u ON u.id = s.tg_user_id
		JOIN events e ON e.id = s.event_id
		WHERE e.name = $1 AND u.linked = true`, eventName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
