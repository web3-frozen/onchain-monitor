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
func New(redisURL string) (*Deduplicator, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
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

// AlreadySent returns true if key was recorded within the given TTL window.
func (d *Deduplicator) AlreadySent(ctx context.Context, key string) bool {
	exists, err := d.rdb.Exists(ctx, key).Result()
	return err == nil && exists > 0
}

// Record marks key as sent with the given TTL.
func (d *Deduplicator) Record(ctx context.Context, key string, ttl time.Duration) {
	d.rdb.Set(ctx, key, "1", ttl)
}
