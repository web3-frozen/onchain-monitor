package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/store"
)

const (
	pollInterval    = 1 * time.Minute
	tvlDropThreshold = 0.10 // 10%
)

// AlertFunc sends a message to a Telegram chat.
type AlertFunc func(chatID int64, message string) error

// Engine is the core monitoring engine that polls registered data sources
// and triggers alerts based on rules.
type Engine struct {
	store     *store.Store
	logger    *slog.Logger
	alertFn   AlertFunc
	sources   map[string]Source
	lastSnap  map[string]*Snapshot
	mu        sync.RWMutex
}

func NewEngine(s *store.Store, logger *slog.Logger, alertFn AlertFunc) *Engine {
	return &Engine{
		store:    s,
		logger:   logger,
		alertFn:  alertFn,
		sources:  make(map[string]Source),
		lastSnap: make(map[string]*Snapshot),
	}
}

// Register adds a data source to the engine.
func (e *Engine) Register(src Source) {
	e.sources[src.Name()] = src
	e.logger.Info("registered source", "source", src.Name())
}

// GetSnapshot returns the latest cached snapshot for a source.
func (e *Engine) GetSnapshot(source string) *Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastSnap[source]
}

// Run starts the polling loop and daily report scheduler.
func (e *Engine) Run(ctx context.Context) {
	// Initial fetch
	e.pollAll(ctx)

	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	// Schedule daily report at 8am HKT (UTC+8 = 00:00 UTC)
	reportTimer := e.nextReportTimer()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			e.pollAll(ctx)
		case <-reportTimer.C:
			e.sendDailyReports(ctx)
			reportTimer = e.nextReportTimer()
		}
	}
}

func (e *Engine) pollAll(ctx context.Context) {
	for name, src := range e.sources {
		snap, err := src.FetchSnapshot()
		if err != nil {
			e.logger.Error("fetch snapshot failed", "source", name, "error", err)
			continue
		}

		e.mu.Lock()
		prev := e.lastSnap[name]
		e.lastSnap[name] = snap
		e.mu.Unlock()

		e.logger.Info("snapshot", "source", name, "tvl", snap.TVL, "price", snap.Price, "apr", snap.APR)

		// Check TVL drop rule
		if prev != nil && prev.TVL > 0 {
			drop := (prev.TVL - snap.TVL) / prev.TVL
			if drop >= tvlDropThreshold {
				e.triggerTVLDropAlert(ctx, name, prev, snap, drop)
			}
		}
	}
}

func (e *Engine) triggerTVLDropAlert(ctx context.Context, source string, prev, curr *Snapshot, dropPct float64) {
	eventName := source + "_tvl_drop"
	chatIDs, err := e.store.GetSubscriberChatIDs(ctx, eventName)
	if err != nil {
		e.logger.Error("get subscribers failed", "event", eventName, "error", err)
		return
	}

	dropAmt := prev.TVL - curr.TVL
	msg := fmt.Sprintf("🚨 %s TVL DROP ALERT\n\n"+
		"TVL dropped by %.1f%% in the last minute!\n"+
		"Previous: $%s\n"+
		"Current:  $%s\n"+
		"Drop:     -$%s\n\n"+
		"🔗 https://app.altura.trade/stats",
		stringToUpper(source),
		dropPct*100,
		formatNum(prev.TVL),
		formatNum(curr.TVL),
		formatNum(dropAmt))

	e.broadcast(chatIDs, msg)
}

func (e *Engine) sendDailyReports(ctx context.Context) {
	for name, src := range e.sources {
		eventName := name + "_daily_report"
		chatIDs, err := e.store.GetSubscriberChatIDs(ctx, eventName)
		if err != nil {
			e.logger.Error("get subscribers failed", "event", eventName, "error", err)
			continue
		}
		if len(chatIDs) == 0 {
			continue
		}

		report, err := src.FetchDailyReport()
		if err != nil {
			e.logger.Error("fetch daily report failed", "source", name, "error", err)
			continue
		}

		e.broadcast(chatIDs, report)
	}
}

func (e *Engine) broadcast(chatIDs []int64, msg string) {
	for _, chatID := range chatIDs {
		if err := e.alertFn(chatID, msg); err != nil {
			e.logger.Error("send alert failed", "chat_id", chatID, "error", err)
		}
	}
}

func (e *Engine) nextReportTimer() *time.Timer {
	hkt := time.FixedZone("HKT", 8*60*60)
	now := time.Now().In(hkt)
	next := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, hkt)
	if now.After(next) {
		next = next.Add(24 * time.Hour)
	}
	duration := time.Until(next)
	e.logger.Info("next daily report", "at", next.Format(time.RFC3339), "in", duration.Round(time.Minute))
	return time.NewTimer(duration)
}

func formatNum(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.2fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%,.2f", math.Round(v*100)/100)
	}
	return fmt.Sprintf("%.2f", v)
}

func stringToUpper(s string) string {
	if len(s) == 0 {
		return s
	}
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
