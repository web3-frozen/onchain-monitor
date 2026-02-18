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

func (s *Store) UnlinkTelegram(ctx context.Context, chatID int64) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM subscriptions WHERE tg_user_id = (SELECT id FROM telegram_users WHERE tg_chat_id = $1)`, chatID)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE telegram_users SET linked = false WHERE tg_chat_id = $1`, chatID)
	return err
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
	ID             int64     `json:"id"`
	TgUserID       int64     `json:"tg_user_id"`
	EventID        int       `json:"event_id"`
	ThresholdPct   float64   `json:"threshold_pct"`
	WindowMinutes  int       `json:"window_minutes"`
	Direction      string    `json:"direction"`
	ReportHour     int       `json:"report_hour"`
	ThresholdValue float64   `json:"threshold_value"`
	Coin           string    `json:"coin"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Store) ListSubscriptions(ctx context.Context, tgChatID int64) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.tg_user_id, s.event_id, s.threshold_pct, s.window_minutes, s.direction, s.report_hour, s.threshold_value, s.coin, s.created_at
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
		if err := rows.Scan(&sub.ID, &sub.TgUserID, &sub.EventID, &sub.ThresholdPct, &sub.WindowMinutes, &sub.Direction, &sub.ReportHour, &sub.ThresholdValue, &sub.Coin, &sub.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *Store) Subscribe(ctx context.Context, tgChatID int64, eventID int, thresholdPct float64, windowMinutes int, direction string, reportHour int, thresholdValue float64, coin string) (*Subscription, error) {
	var sub Subscription
	err := s.pool.QueryRow(ctx, `
		INSERT INTO subscriptions (tg_user_id, event_id, threshold_pct, window_minutes, direction, report_hour, threshold_value, coin)
		SELECT u.id, $2, $3, $4, $5, $6, $7, $8 FROM telegram_users u WHERE u.tg_chat_id = $1
		RETURNING id, tg_user_id, event_id, threshold_pct, window_minutes, direction, report_hour, threshold_value, coin, created_at`,
		tgChatID, eventID, thresholdPct, windowMinutes, direction, reportHour, thresholdValue, coin).
		Scan(&sub.ID, &sub.TgUserID, &sub.EventID, &sub.ThresholdPct, &sub.WindowMinutes, &sub.Direction, &sub.ReportHour, &sub.ThresholdValue, &sub.Coin, &sub.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *Store) UpdateSubscription(ctx context.Context, id int64, thresholdPct float64, windowMinutes int, direction string, reportHour int, thresholdValue float64, coin string) (*Subscription, error) {
	var sub Subscription
	err := s.pool.QueryRow(ctx, `
		UPDATE subscriptions SET threshold_pct = $2, window_minutes = $3, direction = $4, report_hour = $5, threshold_value = $6, coin = $7
		WHERE id = $1
		RETURNING id, tg_user_id, event_id, threshold_pct, window_minutes, direction, report_hour, threshold_value, coin, created_at`,
		id, thresholdPct, windowMinutes, direction, reportHour, thresholdValue, coin).
		Scan(&sub.ID, &sub.TgUserID, &sub.EventID, &sub.ThresholdPct, &sub.WindowMinutes, &sub.Direction, &sub.ReportHour, &sub.ThresholdValue, &sub.Coin, &sub.CreatedAt)
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

// SubscriberConfig holds per-subscriber alert configuration.
type SubscriberConfig struct {
	ChatID         int64
	ThresholdPct   float64
	WindowMinutes  int
	Direction      string
	ThresholdValue float64
	Coin           string
}

func (s *Store) GetSubscribersWithThresholds(ctx context.Context, eventName string) ([]SubscriberConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.tg_chat_id, s.threshold_pct, s.window_minutes, s.direction, s.threshold_value, s.coin
		FROM subscriptions s
		JOIN telegram_users u ON u.id = s.tg_user_id
		JOIN events e ON e.id = s.event_id
		WHERE e.name = $1 AND u.linked = true`, eventName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []SubscriberConfig
	for rows.Next() {
		var c SubscriberConfig
		if err := rows.Scan(&c.ChatID, &c.ThresholdPct, &c.WindowMinutes, &c.Direction, &c.ThresholdValue, &c.Coin); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

// DailyReportSubscriber holds per-subscriber daily report config.
type DailyReportSubscriber struct {
	ChatID     int64
	ReportHour int
}

func (s *Store) GetDailyReportSubscribers(ctx context.Context, eventName string, hour int) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.tg_chat_id
		FROM subscriptions s
		JOIN telegram_users u ON u.id = s.tg_user_id
		JOIN events e ON e.id = s.event_id
		WHERE e.name = $1 AND u.linked = true AND s.report_hour = $2`, eventName, hour)
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

// CountSubscriptions returns the number of active subscriptions for an event.
func (s *Store) CountSubscriptions(ctx context.Context, eventName string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM subscriptions s
		JOIN events e ON e.id = s.event_id
		WHERE e.name = $1`, eventName).Scan(&count)
	return count, err
}

// CountLinkedUsers returns the number of linked Telegram users.
func (s *Store) CountLinkedUsers(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM telegram_users WHERE linked = true`).Scan(&count)
	return count, err
}

// --- Liquidation Events ---

// LiquidationEvent represents a single forced liquidation from an exchange.
type LiquidationEvent struct {
	Symbol    string
	Side      string // "LONG" or "SHORT" (which side got liquidated)
	Price     float64
	Quantity  float64
	USDValue  float64
	Exchange  string
	EventTime time.Time
}

// InsertLiquidationEvent stores a liquidation event.
func (s *Store) InsertLiquidationEvent(ctx context.Context, e *LiquidationEvent) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO liquidation_events (symbol, side, price, quantity, usd_value, exchange, event_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.Symbol, e.Side, e.Price, e.Quantity, e.USDValue, e.Exchange, e.EventTime)
	return err
}

// InsertLiquidationEvents batch-inserts liquidation events.
func (s *Store) InsertLiquidationEvents(ctx context.Context, events []LiquidationEvent) error {
	if len(events) == 0 {
		return nil
	}
	// Use a single transaction for batch efficiency
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, e := range events {
		_, err := tx.Exec(ctx, `
			INSERT INTO liquidation_events (symbol, side, price, quantity, usd_value, exchange, event_time)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			e.Symbol, e.Side, e.Price, e.Quantity, e.USDValue, e.Exchange, e.EventTime)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// MaxPainResult holds the calculated max pain price for one side.
type MaxPainResult struct {
	PriceBin float64
	USDTotal float64
}

// QueryMaxPain calculates the liquidation max pain for a symbol and time window.
// binSize: price bin width (e.g., 100 for BTC, 10 for ETH).
// Returns (longMaxPain, shortMaxPain, error).
func (s *Store) QueryMaxPain(ctx context.Context, symbol string, window time.Duration, binSize float64) (*MaxPainResult, *MaxPainResult, error) {
	since := time.Now().Add(-window)

	var longMP MaxPainResult
	err := s.pool.QueryRow(ctx, `
		SELECT ROUND(price / $3) * $3 AS price_bin, SUM(usd_value) AS total
		FROM liquidation_events
		WHERE symbol = $1 AND event_time > $2 AND side = 'LONG'
		GROUP BY price_bin
		ORDER BY total DESC
		LIMIT 1`, symbol, since, binSize).Scan(&longMP.PriceBin, &longMP.USDTotal)
	if err != nil {
		longMP = MaxPainResult{} // no data is ok
	}

	var shortMP MaxPainResult
	err = s.pool.QueryRow(ctx, `
		SELECT ROUND(price / $3) * $3 AS price_bin, SUM(usd_value) AS total
		FROM liquidation_events
		WHERE symbol = $1 AND event_time > $2 AND side = 'SHORT'
		GROUP BY price_bin
		ORDER BY total DESC
		LIMIT 1`, symbol, since, binSize).Scan(&shortMP.PriceBin, &shortMP.USDTotal)
	if err != nil {
		shortMP = MaxPainResult{} // no data is ok
	}

	return &longMP, &shortMP, nil
}

// GetCurrentPrice returns the latest liquidation event price for a symbol (rough proxy for current price).
func (s *Store) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	var price float64
	err := s.pool.QueryRow(ctx, `
		SELECT price FROM liquidation_events
		WHERE symbol = $1
		ORDER BY event_time DESC
		LIMIT 1`, symbol).Scan(&price)
	return price, err
}

// CountLiquidationEvents returns event count for a symbol within a window.
func (s *Store) CountLiquidationEvents(ctx context.Context, symbol string, window time.Duration) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM liquidation_events
		WHERE symbol = $1 AND event_time > $2`, symbol, time.Now().Add(-window)).Scan(&count)
	return count, err
}

// CleanupOldLiquidationEvents deletes events older than the given duration.
func (s *Store) CleanupOldLiquidationEvents(ctx context.Context, maxAge time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM liquidation_events WHERE event_time < $1`, time.Now().Add(-maxAge))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Pool exposes the underlying connection pool for use by other packages.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}
