package dedup

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func setupTestDedup(t *testing.T) (*Deduplicator, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	d, err := New("redis://"+mr.Addr(), "")
	if err != nil {
		mr.Close()
		t.Fatalf("New: %v", err)
	}
	return d, mr
}

func TestAlreadySentNewKey(t *testing.T) {
	d, mr := setupTestDedup(t)
	defer mr.Close()
	defer d.Close()

	ctx := context.Background()
	if d.AlreadySent(ctx, "test:key:1") {
		t.Error("AlreadySent should return false for new key")
	}
}

func TestRecordAndAlreadySent(t *testing.T) {
	d, mr := setupTestDedup(t)
	defer mr.Close()
	defer d.Close()

	ctx := context.Background()
	d.Record(ctx, "test:key:2")

	if !d.AlreadySent(ctx, "test:key:2") {
		t.Error("AlreadySent should return true after Record")
	}
}

func TestClear(t *testing.T) {
	d, mr := setupTestDedup(t)
	defer mr.Close()
	defer d.Close()

	ctx := context.Background()
	d.Record(ctx, "test:key:3")

	if !d.AlreadySent(ctx, "test:key:3") {
		t.Fatal("should be sent after Record")
	}

	d.Clear(ctx, "test:key:3")
	if d.AlreadySent(ctx, "test:key:3") {
		t.Error("AlreadySent should return false after Clear")
	}
}

func TestClearByPattern(t *testing.T) {
	d, mr := setupTestDedup(t)
	defer mr.Close()
	defer d.Close()

	ctx := context.Background()
	d.Record(ctx, "alert:123:metric1")
	d.Record(ctx, "alert:123:metric2")
	d.Record(ctx, "alert:456:metric1")

	d.ClearByPattern(ctx, "alert:123:*")

	if d.AlreadySent(ctx, "alert:123:metric1") {
		t.Error("key alert:123:metric1 should be cleared")
	}
	if d.AlreadySent(ctx, "alert:123:metric2") {
		t.Error("key alert:123:metric2 should be cleared")
	}
	if !d.AlreadySent(ctx, "alert:456:metric1") {
		t.Error("key alert:456:metric1 should NOT be cleared")
	}
}

func TestAlreadySentFailClosed(t *testing.T) {
	d, mr := setupTestDedup(t)
	defer d.Close()

	// Stop Redis to simulate failure
	mr.Close()

	ctx := context.Background()
	if !d.AlreadySent(ctx, "any:key") {
		t.Error("AlreadySent should return true (fail-closed) when Redis is down")
	}
}
