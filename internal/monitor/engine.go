package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/store"
)

const (
	pollInterval   = 1 * time.Minute
	dropThreshold  = 0.10 // 10%
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

// SourceNames returns names of all registered sources.
func (e *Engine) SourceNames() []string {
	names := make([]string, 0, len(e.sources))
	for n := range e.sources {
		names = append(names, n)
	}
	return names
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

		e.logger.Info("snapshot", "source", name, "metrics", snap.Metrics)

		// Check all metrics for drops ≥ threshold
		if prev != nil {
			for metric, currVal := range snap.Metrics {
				prevVal, ok := prev.Metrics[metric]
				if !ok || prevVal <= 0 {
					continue
				}
				drop := (prevVal - currVal) / prevVal
				if drop >= dropThreshold {
					e.triggerDropAlert(ctx, src, metric, prevVal, currVal, drop)
				}
			}
		}
	}
}

func (e *Engine) triggerDropAlert(ctx context.Context, src Source, metric string, prevVal, currVal, dropPct float64) {
	eventName := src.Name() + "_drop"
	chatIDs, err := e.store.GetSubscriberChatIDs(ctx, eventName)
	if err != nil {
		e.logger.Error("get subscribers failed", "event", eventName, "error", err)
		return
	}

	dropAmt := prevVal - currVal
	msg := fmt.Sprintf("🚨 %s %s DROP ALERT\n\n"+
		"%s dropped by %.1f%% in the last minute!\n"+
		"Previous: $%s\n"+
		"Current:  $%s\n"+
		"Drop:     -$%s\n\n"+
		"🔗 %s",
		stringToUpper(src.Name()),
		stringToUpper(metric),
		stringToUpper(metric),
		dropPct*100,
		formatNum(prevVal),
		formatNum(currVal),
		formatNum(dropAmt),
		src.URL())

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
		return addCommas(fmt.Sprintf("%.2f", math.Round(v*100)/100))
	}
	return fmt.Sprintf("%.4f", v)
}

func addCommas(s string) string {
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	n := len(intPart)
	if n <= 3 {
		if len(parts) == 2 {
			return intPart + "." + parts[1]
		}
		return intPart
	}
	var result []byte
	for i, c := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	if len(parts) == 2 {
		return string(result) + "." + parts[1]
	}
	return string(result)
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
