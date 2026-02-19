package dedup

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Deduplicator checks and records whether an alert has been sent recently.
type Deduplicator struct {
	rdb *redis.Client
}

// New creates a Deduplicator backed by Redis.
func New(redisURL, password string) (*Deduplicator, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	if password != "" {
		opts.Password = password
	}
	rdb := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Deduplicator{rdb: rdb}, nil
}

// Close shuts down the Redis connection.
func (d *Deduplicator) Close() error {
	return d.rdb.Close()
}

// AlreadySent returns true if key was recorded, or if Redis is unreachable
// (fail-closed: assume sent to avoid duplicate alerts during Redis downtime).
func (d *Deduplicator) AlreadySent(ctx context.Context, key string) bool {
	exists, err := d.rdb.Exists(ctx, key).Result()
	if err != nil {
		return true // fail closed — suppress alerts when Redis is down
	}
	return exists > 0
}

// Record marks key as sent permanently (no expiry).
func (d *Deduplicator) Record(ctx context.Context, key string) {
	d.rdb.Set(ctx, key, "1", 0) // 0 = no expiry
}

// Clear removes a dedup key so the alert can fire again when the condition resets.
func (d *Deduplicator) Clear(ctx context.Context, key string) {
	d.rdb.Del(ctx, key) //nolint:errcheck
}

// ClearByPattern removes all dedup keys matching a glob pattern (e.g., "*:12345:*").
func (d *Deduplicator) ClearByPattern(ctx context.Context, pattern string) {
	iter := d.rdb.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		d.rdb.Del(ctx, iter.Val()) //nolint:errcheck
	}
}
