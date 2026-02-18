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
	maxHistoryLen  = 60 // keep 60 minutes of snapshots
)

// AlertFunc sends a message to a Telegram chat.
type AlertFunc func(chatID int64, message string) error

// Engine is the core monitoring engine that polls registered data sources
// and triggers alerts based on rules.
type Engine struct {
	store       *store.Store
	logger      *slog.Logger
	alertFn     AlertFunc
	sources     map[string]Source
	snapHistory map[string][]*Snapshot
	lastAlerted map[string]time.Time
	mu          sync.RWMutex
}

func NewEngine(s *store.Store, logger *slog.Logger, alertFn AlertFunc) *Engine {
	return &Engine{
		store:       s,
		logger:      logger,
		alertFn:     alertFn,
		sources:     make(map[string]Source),
		snapHistory: make(map[string][]*Snapshot),
		lastAlerted: make(map[string]time.Time),
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

// Chains returns a deduplicated list of chains from registered sources.
func (e *Engine) Chains() []string {
	seen := make(map[string]bool)
	chains := make([]string, 0)
	for _, src := range e.sources {
		c := src.Chain()
		if !seen[c] {
			seen[c] = true
			chains = append(chains, c)
		}
	}
	return chains
}

// GetSnapshot returns the latest cached snapshot for a source.
func (e *Engine) GetSnapshot(source string) *Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	history := e.snapHistory[source]
	if len(history) == 0 {
		return nil
	}
	return history[len(history)-1]
}

// Run starts the polling loop and daily report scheduler.
func (e *Engine) Run(ctx context.Context) {
	// Initial fetch
	e.pollAll(ctx)

	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	// Check daily reports every minute alongside polls
	var lastReportHour int = -1

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			e.pollAll(ctx)

			// Check if any subscribers are due their daily report this hour
			utc8 := time.FixedZone("UTC+8", 8*60*60)
			now := time.Now().In(utc8)
			currentHour := now.Hour()
			if currentHour != lastReportHour {
				lastReportHour = currentHour
				e.sendDueReports(ctx, currentHour)
			}
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
		history := e.snapHistory[name]
		history = append(history, snap)
		if len(history) > maxHistoryLen {
			history = history[len(history)-maxHistoryLen:]
		}
		e.snapHistory[name] = history
		e.mu.Unlock()

		e.logger.Info("snapshot", "source", name, "metrics", snap.Metrics)

		// Per-subscriber threshold checking
		eventName := name + "_metric_alert"
		subscribers, err := e.store.GetSubscribersWithThresholds(ctx, eventName)
		if err != nil {
			e.logger.Error("get subscribers with thresholds failed", "event", eventName, "error", err)
			continue
		}

		e.mu.RLock()
		hist := e.snapHistory[name]
		e.mu.RUnlock()

		for _, sub := range subscribers {
			if sub.WindowMinutes < 1 || sub.WindowMinutes >= len(hist) {
				continue
			}
			pastSnap := hist[len(hist)-1-sub.WindowMinutes]
			threshold := sub.ThresholdPct / 100

			for metric, currVal := range snap.Metrics {
				prevVal, ok := pastSnap.Metrics[metric]
				if !ok || prevVal <= 0 {
					continue
				}

				var change float64
				if sub.Direction == "increase" {
					change = (currVal - prevVal) / prevVal
				} else {
					change = (prevVal - currVal) / prevVal
				}

				if change >= threshold {
					alertKey := fmt.Sprintf("%d:%s:%s:%s", sub.ChatID, name, metric, sub.Direction)
					if lastTime, ok := e.lastAlerted[alertKey]; ok {
						if time.Since(lastTime) < time.Duration(sub.WindowMinutes)*time.Minute {
							continue
						}
					}
					e.sendMetricAlertToUser(sub.ChatID, src, metric, prevVal, currVal, change, sub.WindowMinutes, sub.Direction)
					e.lastAlerted[alertKey] = time.Now()
				}
			}
		}
	}
}

func (e *Engine) sendMetricAlertToUser(chatID int64, src Source, metric string, prevVal, currVal, changePct float64, windowMin int, direction string) {
	dirLabel := "DROP"
	verb := "dropped"
	diffSign := "-"
	if direction == "increase" {
		dirLabel = "INCREASE"
		verb = "increased"
		diffSign = "+"
	}
	diff := prevVal - currVal
	if diff < 0 {
		diff = -diff
	}
	msg := fmt.Sprintf("🚨 %s %s %s ALERT\n\n"+
		"%s %s by %.1f%% in the last %d minute(s)!\n"+
		"Previous: $%s\n"+
		"Current:  $%s\n"+
		"Change:   %s$%s\n\n"+
		"🔗 %s",
		stringToUpper(src.Name()),
		stringToUpper(metric),
		dirLabel,
		stringToUpper(metric),
		verb,
		changePct*100,
		windowMin,
		formatNum(prevVal),
		formatNum(currVal),
		diffSign,
		formatNum(diff),
		src.URL())

	if err := e.alertFn(chatID, msg); err != nil {
		e.logger.Error("send alert failed", "chat_id", chatID, "error", err)
	}
}

func (e *Engine) sendDueReports(ctx context.Context, hour int) {
	for name, src := range e.sources {
		eventName := name + "_daily_report"
		chatIDs, err := e.store.GetDailyReportSubscribers(ctx, eventName, hour)
		if err != nil {
			e.logger.Error("get daily report subscribers failed", "event", eventName, "hour", hour, "error", err)
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
		e.logger.Info("sent daily reports", "source", name, "hour", hour, "recipients", len(chatIDs))
	}
}

func (e *Engine) broadcast(chatIDs []int64, msg string) {
	for _, chatID := range chatIDs {
		if err := e.alertFn(chatID, msg); err != nil {
			e.logger.Error("send alert failed", "chat_id", chatID, "error", err)
		}
	}
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
