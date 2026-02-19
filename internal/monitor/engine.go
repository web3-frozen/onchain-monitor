package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/dedup"
	"github.com/web3-frozen/onchain-monitor/internal/metrics"
	"github.com/web3-frozen/onchain-monitor/internal/store"
)

const (
	pollInterval     = 1 * time.Minute
	maxHistoryLen    = 60 // keep 60 minutes of snapshots
	fetchTimeout     = 30 * time.Second
)

// AlertFunc sends a message to a Telegram chat.
type AlertFunc func(chatID int64, message string) error

// Engine is the core monitoring engine that polls registered data sources
// and triggers alerts based on rules.
type Engine struct {
	store       *store.Store
	logger      *slog.Logger
	alertFn     AlertFunc
	dedup       *dedup.Deduplicator
	sources     map[string]Source
	snapHistory map[string][]*Snapshot
	mu          sync.RWMutex
}

func NewEngine(s *store.Store, logger *slog.Logger, alertFn AlertFunc, dd *dedup.Deduplicator) *Engine {
	return &Engine{
		store:       s,
		logger:      logger,
		alertFn:     alertFn,
		dedup:       dd,
		sources:     make(map[string]Source),
		snapHistory: make(map[string][]*Snapshot),
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
	e.refreshBusinessGauges(ctx)

	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			e.pollAll(ctx)
			e.refreshBusinessGauges(ctx)

			// Check if any subscribers are due their daily report this hour
			utc8 := time.FixedZone("UTC+8", 8*60*60)
			now := time.Now().In(utc8)
			e.sendDueReports(ctx, now.Hour())
		}
	}
}

// refreshBusinessGauges updates subscription and user count gauges.
func (e *Engine) refreshBusinessGauges(ctx context.Context) {
	events, err := e.store.ListEvents(ctx)
	if err != nil {
		e.logger.Error("refresh business gauges: list events failed", "error", err)
		return
	}
	for _, ev := range events {
		count, err := e.store.CountSubscriptions(ctx, ev.Name)
		if err != nil {
			continue
		}
		metrics.SubscriptionsActive.WithLabelValues(ev.Name).Set(float64(count))
	}
	linkedCount, err := e.store.CountLinkedUsers(ctx)
	if err != nil {
		e.logger.Error("refresh business gauges: count linked users failed", "error", err)
		return
	}
	metrics.TelegramLinkedUsers.Set(float64(linkedCount))
}

// fetchWithTimeout wraps a FetchSnapshot call with a deadline so one slow
// source cannot block the entire poll cycle.
func fetchWithTimeout(fn func() (*Snapshot, error), timeout time.Duration) (*Snapshot, error) {
	type result struct {
		snap *Snapshot
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		s, e := fn()
		ch <- result{s, e}
	}()
	select {
	case r := <-ch:
		return r.snap, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("fetch timed out after %v", timeout)
	}
}

func (e *Engine) pollAll(ctx context.Context) {
	for name, src := range e.sources {
		pollStart := time.Now()
		snap, err := fetchWithTimeout(src.FetchSnapshot, fetchTimeout)
		pollDur := time.Since(pollStart).Seconds()
		metrics.PollDuration.WithLabelValues(name).Observe(pollDur)

		if err != nil {
			metrics.PollTotal.WithLabelValues(name, "error").Inc()
			e.logger.Error("fetch snapshot failed", "source", name, "error", err)
			continue
		}
		metrics.PollTotal.WithLabelValues(name, "success").Inc()
		metrics.PollLastSuccess.WithLabelValues(name).Set(float64(time.Now().Unix()))

		e.mu.Lock()
		history := e.snapHistory[name]
		history = append(history, snap)
		if len(history) > maxHistoryLen {
			history = history[len(history)-maxHistoryLen:]
		}
		e.snapHistory[name] = history
		metrics.SnapshotCount.WithLabelValues(name).Set(float64(len(history)))
		metrics.SnapshotAge.WithLabelValues(name).Set(time.Since(snap.FetchedAt).Seconds())
		e.mu.Unlock()

		// Export business metric values as Prometheus gauges
		for metricName, val := range snap.Metrics {
			metrics.MetricValue.WithLabelValues(name, metricName).Set(val)
		}

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
			// Value-based alerts (higher/lower than threshold_value)
			if sub.ThresholdValue > 0 && (sub.Direction == "higher" || sub.Direction == "lower") {
				for metric, currVal := range snap.Metrics {
					alertKey := fmt.Sprintf("%d:%s:%s:%s:%.0f", sub.ChatID, name, metric, sub.Direction, sub.ThresholdValue)
					var triggered bool
					if sub.Direction == "higher" && currVal > sub.ThresholdValue {
						triggered = true
					} else if sub.Direction == "lower" && currVal < sub.ThresholdValue {
						triggered = true
					}
					if triggered {
						if e.dedup.AlreadySent(ctx, alertKey) {
							metrics.AlertsDeduplicatedTotal.WithLabelValues(name, "value_alert").Inc()
							continue
						}
						e.sendValueAlert(sub.ChatID, src, metric, currVal, sub.ThresholdValue, sub.Direction)
						e.dedup.Record(ctx, alertKey)
					} else {
						// Condition no longer met — clear so alert can fire again next time
						e.dedup.Clear(ctx, alertKey)
					}
				}
				continue
			}

			// Percentage-based alerts (drop/increase)
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
					if e.dedup.AlreadySent(ctx, alertKey) {
						metrics.AlertsDeduplicatedTotal.WithLabelValues(name, "metric_alert").Inc()
						continue
					}
					e.sendMetricAlertToUser(sub.ChatID, src, metric, prevVal, currVal, change, sub.WindowMinutes, sub.Direction)
					e.dedup.Record(ctx, alertKey)
				} else {
					// Condition no longer met — clear so alert can fire again
					alertKey := fmt.Sprintf("%d:%s:%s:%s", sub.ChatID, name, metric, sub.Direction)
					e.dedup.Clear(ctx, alertKey)
				}
			}
		}
	}

	// Check maxpain alerts separately (uses cross-source data)
	e.checkMaxpainAlerts(ctx)

	// Check merkl yield opportunity alerts
	e.checkMerklAlerts(ctx)

	// Check turtle yield opportunity alerts
	e.checkTurtleAlerts(ctx)

	// Check Binance price alerts
	e.checkBinancePriceAlerts(ctx)
}

// checkMaxpainAlerts checks if current prices have crossed liquidation max pain levels.
func (e *Engine) checkMaxpainAlerts(ctx context.Context) {
	maxpainSrc, ok := e.sources["maxpain"]
	if !ok {
		return
	}

	// Type-assert to access interval-aware methods
	type intervalScraper interface {
		GetEntry(symbol, interval string) (MaxPainEntry, bool)
		ScrapeIntervals(intervals []string) error
	}
	mp, ok := maxpainSrc.(intervalScraper)
	if !ok {
		return
	}

	subscribers, err := e.store.GetSubscribersWithThresholds(ctx, "general_maxpain_alert")
	if err != nil || len(subscribers) == 0 {
		return
	}

	// Collect unique intervals needed and scrape them in one Chrome session
	needed := make(map[string]bool)
	for _, sub := range subscribers {
		iv := IntervalFromMinutes(sub.WindowMinutes)
		if iv != "24h" { // 24h already scraped in FetchSnapshot
			needed[iv] = true
		}
	}
	if len(needed) > 0 {
		extras := make([]string, 0, len(needed))
		for iv := range needed {
			extras = append(extras, iv)
		}
		if err := mp.ScrapeIntervals(extras); err != nil {
			e.logger.Warn("scrape maxpain intervals failed", "error", err)
		}
	}

	for _, sub := range subscribers {
		coin := strings.ToUpper(sub.Coin)
		if coin == "" {
			continue
		}
		interval := IntervalFromMinutes(sub.WindowMinutes)

		entry, ok := mp.GetEntry(coin, interval)
		if !ok || entry.Price <= 0 {
			continue
		}

		var maxpainPrice float64
		side := strings.ToLower(sub.Direction)
		switch side {
		case "long":
			maxpainPrice = entry.MaxLongLiquidationPrice
		case "short":
			maxpainPrice = entry.MaxShortLiquidationPrice
		default:
			continue
		}
		if maxpainPrice <= 0 {
			continue
		}

		// Calculate distance percentage from current price to maxpain
		dist := math.Abs(entry.Price-maxpainPrice) / entry.Price * 100

		// Alert when price is within threshold_value% of maxpain (default 1%)
		threshold := sub.ThresholdValue
		if threshold <= 0 {
			threshold = 1.0
		}

		if dist <= threshold {
			alertKey := fmt.Sprintf("maxpain:%d:%s:%s:%s", sub.ChatID, coin, side, interval)
			if e.dedup.AlreadySent(ctx, alertKey) {
				metrics.AlertsDeduplicatedTotal.WithLabelValues("maxpain", "maxpain_alert").Inc()
				continue
			}
			e.sendMaxpainAlert(sub.ChatID, maxpainSrc, coin, side, interval, entry.Price, maxpainPrice, dist)
			e.dedup.Record(ctx, alertKey)
		} else {
			alertKey := fmt.Sprintf("maxpain:%d:%s:%s:%s", sub.ChatID, coin, side, interval)
			e.dedup.Clear(ctx, alertKey)
		}
	}
}

func (e *Engine) sendMaxpainAlert(chatID int64, src Source, coin, side, interval string, price, maxpainPrice, dist float64) {
	sideLabel := "LONG"
	if side == "short" {
		sideLabel = "SHORT"
	}
	msg := fmt.Sprintf("🚨 %s %s MAX PAIN ALERT (%s)\n\n"+
		"%s price ($%s) is within %.1f%% of %s max pain ($%s)!\n\n"+
		"Current Price: $%s\n"+
		"%s Max Pain:   $%s\n"+
		"Interval:      %s\n\n"+
		"🔗 %s?type=%s",
		coin, sideLabel, interval,
		coin, formatNum(price), dist, sideLabel, formatNum(maxpainPrice),
		formatNum(price),
		sideLabel, formatNum(maxpainPrice),
		interval,
		src.URL(), interval)

	if err := e.alertFn(chatID, msg); err != nil {
		metrics.AlertsFailedTotal.WithLabelValues("maxpain", "maxpain_alert").Inc()
		e.logger.Error("send maxpain alert failed", "chat_id", chatID, "error", err)
	} else {
		metrics.AlertsSentTotal.WithLabelValues("maxpain", "maxpain_alert").Inc()
		e.logNotification(chatID, "maxpain", "general_maxpain_alert",
			fmt.Sprintf("%s %s within %.1f%% of max pain $%s (interval: %s)", coin, side, dist, formatNum(maxpainPrice), interval))
	}
}

// MerklOpp holds opportunity data for Merkl yield alerts.
type MerklOpp struct {
	ID         string
	Name       string
	Action     string
	TVL        float64
	APR        float64
	ChainName  string
	Protocol   string
	DepositURL string
	MerklURL   string
	Stablecoin bool
}

// checkMerklAlerts checks for new yield opportunities matching subscriber criteria.
func (e *Engine) checkMerklAlerts(ctx context.Context) {
	merklSrc, ok := e.sources["merkl"]
	if !ok {
		return
	}

	subscribers, err := e.store.GetSubscribersWithThresholds(ctx, "general_merkl_alert")
	if err != nil || len(subscribers) == 0 {
		return
	}

	type oppGetter interface {
		GetFilteredOpportunities(minAPR, minTVL float64, action, stableFilter string) []MerklOpp
	}

	getter, ok := merklSrc.(oppGetter)
	if !ok {
		return
	}

	for _, sub := range subscribers {
		minAPR := sub.ThresholdValue
		if minAPR <= 0 {
			minAPR = 10
		}
		minTVL := sub.ThresholdPct * 1_000_000
		if minTVL <= 0 {
			minTVL = 1_000_000
		}
		action := strings.ToUpper(sub.Coin)
		if action == "" {
			action = "ALL"
		}
		stableFilter := sub.Direction
		if stableFilter == "" {
			stableFilter = "any"
		}

		opps := getter.GetFilteredOpportunities(minAPR, minTVL, action, stableFilter)

		// Collect new (unseen) opportunities
		var newOpps []MerklOpp
		for _, opp := range opps {
			alertKey := fmt.Sprintf("merkl:%d:%s", sub.ChatID, opp.ID)
			if e.dedup.AlreadySent(ctx, alertKey) {
				continue
			}
			newOpps = append(newOpps, opp)
		}

		if len(newOpps) == 0 {
			continue
		}

		// Send as a single grouped message
		e.sendMerklGroupedAlert(sub.ChatID, newOpps)

		// Mark all as alerted permanently — each opportunity alerts only once per user
		for _, opp := range newOpps {
			alertKey := fmt.Sprintf("merkl:%d:%s", sub.ChatID, opp.ID)
			e.dedup.Record(ctx, alertKey)
		}
	}
}

func (e *Engine) sendMerklGroupedAlert(chatID int64, opps []MerklOpp) {
	// Sort opportunities by chain order: general → hyperliquid → monad, then alphabetically
	chainOrder := map[string]int{
		"ethereum": 0, "base": 1, "arbitrum": 2, "optimism": 3,
		"hyperevm": 10, "hyperevmmainnet": 10,
		"monad": 20, "monadtestnet": 21,
	}
	sort.SliceStable(opps, func(i, j int) bool {
		ci := strings.ToLower(strings.ReplaceAll(opps[i].ChainName, " ", ""))
		cj := strings.ToLower(strings.ReplaceAll(opps[j].ChainName, " ", ""))
		oi, oki := chainOrder[ci]
		oj, okj := chainOrder[cj]
		if !oki {
			oi = 50
		}
		if !okj {
			oj = 50
		}
		if oi != oj {
			return oi < oj
		}
		return opps[i].APR > opps[j].APR
	})

	var b strings.Builder

	if len(opps) == 1 {
		b.WriteString("💰 New Yield Opportunity\n\n")
	} else {
		b.WriteString(fmt.Sprintf("💰 %d New Yield Opportunities\n\n", len(opps)))
	}

	for i, opp := range opps {
		stable := ""
		if opp.Stablecoin {
			stable = " 🟢"
		}
		b.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, opp.Name, stable))
		b.WriteString(fmt.Sprintf("   APR: %.1f%% | TVL: $%s\n", opp.APR, formatMerklTVL(opp.TVL)))
		b.WriteString(fmt.Sprintf("   %s · %s · %s\n", opp.ChainName, opp.Action, opp.Protocol))
		b.WriteString(fmt.Sprintf("   🔗 %s\n", opp.MerklURL))
		if i < len(opps)-1 {
			b.WriteString("\n")
		}
	}

	if err := e.alertFn(chatID, b.String()); err != nil {
		metrics.AlertsFailedTotal.WithLabelValues("merkl", "merkl_alert").Inc()
		e.logger.Error("send merkl alert failed", "chat_id", chatID, "error", err)
	} else {
		metrics.AlertsSentTotal.WithLabelValues("merkl", "merkl_alert").Inc()
		names := make([]string, len(opps))
		for i, o := range opps {
			names[i] = o.Name
		}
		e.logNotification(chatID, "merkl", "general_merkl_alert",
			fmt.Sprintf("%d new opportunities: %s", len(opps), strings.Join(names, ", ")))
	}
}

// TurtleOpp holds opportunity data for Turtle yield alerts.
type TurtleOpp struct {
	ID           string
	Name         string
	Type         string
	TVL          float64
	APR          float64
	ChainName    string
	Organization string
	Token        string
	TurtleURL    string
	Stablecoin   bool
}

// checkTurtleAlerts checks for new Turtle yield opportunities matching subscriber criteria.
func (e *Engine) checkTurtleAlerts(ctx context.Context) {
	turtleSrc, ok := e.sources["turtle"]
	if !ok {
		return
	}

	subscribers, err := e.store.GetSubscribersWithThresholds(ctx, "general_turtle_alert")
	if err != nil || len(subscribers) == 0 {
		return
	}

	type oppGetter interface {
		GetFilteredOpportunities(minAPR, minTVL float64, tagFilter, tokenFilter string) []TurtleOpp
	}

	getter, ok := turtleSrc.(oppGetter)
	if !ok {
		return
	}

	for _, sub := range subscribers {
		minAPR := sub.ThresholdValue
		if minAPR <= 0 {
			minAPR = 10
		}
		minTVL := sub.ThresholdPct * 1_000_000
		if minTVL <= 0 {
			minTVL = 1_000_000
		}
		tagFilter := sub.Coin
		if tagFilter == "" {
			tagFilter = "ALL"
		}
		tokenFilter := sub.Direction
		if tokenFilter == "" {
			tokenFilter = "all"
		}

		opps := getter.GetFilteredOpportunities(minAPR, minTVL, tagFilter, tokenFilter)

		var newOpps []TurtleOpp
		for _, opp := range opps {
			alertKey := fmt.Sprintf("turtle:%d:%s", sub.ChatID, opp.ID)
			if e.dedup.AlreadySent(ctx, alertKey) {
				continue
			}
			newOpps = append(newOpps, opp)
		}

		if len(newOpps) == 0 {
			continue
		}

		e.sendTurtleGroupedAlert(sub.ChatID, newOpps)

		for _, opp := range newOpps {
			alertKey := fmt.Sprintf("turtle:%d:%s", sub.ChatID, opp.ID)
			e.dedup.Record(ctx, alertKey)
		}
	}
}

func (e *Engine) sendTurtleGroupedAlert(chatID int64, opps []TurtleOpp) {
	sort.SliceStable(opps, func(i, j int) bool {
		return opps[i].APR > opps[j].APR
	})

	var b strings.Builder
	if len(opps) == 1 {
		b.WriteString("🐢 New Turtle Yield Opportunity\n\n")
	} else {
		b.WriteString(fmt.Sprintf("🐢 %d New Turtle Yield Opportunities\n\n", len(opps)))
	}

	for i, opp := range opps {
		stable := ""
		if opp.Stablecoin {
			stable = " 🟢"
		}
		b.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, opp.Name, stable))
		b.WriteString(fmt.Sprintf("   Yield: %.1f%% | TVL: $%s\n", opp.APR, formatMerklTVL(opp.TVL)))
		b.WriteString(fmt.Sprintf("   %s · %s · %s\n", opp.ChainName, opp.Type, opp.Organization))
		b.WriteString(fmt.Sprintf("   🔗 %s\n", opp.TurtleURL))
		if i < len(opps)-1 {
			b.WriteString("\n")
		}
	}

	if err := e.alertFn(chatID, b.String()); err != nil {
		metrics.AlertsFailedTotal.WithLabelValues("turtle", "turtle_alert").Inc()
		e.logger.Error("send turtle alert failed", "chat_id", chatID, "error", err)
	} else {
		metrics.AlertsSentTotal.WithLabelValues("turtle", "turtle_alert").Inc()
		names := make([]string, len(opps))
		for i, o := range opps {
			names[i] = o.Name
		}
		e.logNotification(chatID, "turtle", "general_turtle_alert",
			fmt.Sprintf("%d new opportunities: %s", len(opps), strings.Join(names, ", ")))
	}
}

// checkBinancePriceAlerts checks Binance prices against subscriber thresholds.
func (e *Engine) checkBinancePriceAlerts(ctx context.Context) {
	binanceSrc, ok := e.sources["binance"]
	if !ok {
		return
	}

	type priceFetcher interface {
		FetchPrice(symbol string) (float64, error)
	}
	fetcher, ok := binanceSrc.(priceFetcher)
	if !ok {
		return
	}

	subscribers, err := e.store.GetSubscribersWithThresholds(ctx, "general_binance_price_alert")
	if err != nil || len(subscribers) == 0 {
		return
	}

	// Cache prices per symbol to avoid duplicate API calls
	priceCache := make(map[string]float64)

	for _, sub := range subscribers {
		coin := strings.ToUpper(sub.Coin)
		if coin == "" {
			coin = "BTC"
		}
		if sub.ThresholdValue <= 0 {
			continue
		}

		price, cached := priceCache[coin]
		if !cached {
			p, err := fetcher.FetchPrice(coin)
			if err != nil {
				e.logger.Warn("binance price fetch failed", "coin", coin, "error", err)
				continue
			}
			price = p
			priceCache[coin] = price
		}

		direction := strings.ToLower(sub.Direction)
		var triggered bool
		switch direction {
		case "increase":
			triggered = price >= sub.ThresholdValue
		case "decrease":
			triggered = price <= sub.ThresholdValue
		default:
			continue
		}

		alertKey := fmt.Sprintf("binance:%d:%s:%s:%.2f", sub.ChatID, coin, direction, sub.ThresholdValue)
		if triggered {
			if e.dedup.AlreadySent(ctx, alertKey) {
				metrics.AlertsDeduplicatedTotal.WithLabelValues("binance", "binance_price_alert").Inc()
				continue
			}
			e.sendBinancePriceAlert(sub.ChatID, binanceSrc, coin, price, sub.ThresholdValue, direction)
			e.dedup.Record(ctx, alertKey)
		} else {
			e.dedup.Clear(ctx, alertKey)
		}
	}
}

func (e *Engine) sendBinancePriceAlert(chatID int64, src Source, coin string, price, targetPrice float64, direction string) {
	dirLabel := "⬆️ INCREASE"
	if direction == "decrease" {
		dirLabel = "⬇️ DECREASE"
	}
	msg := fmt.Sprintf("🚨 %s/USDT PRICE %s ALERT\n\n"+
		"%s has reached your target price!\n\n"+
		"Current Price: $%s\n"+
		"Target Price:  $%s\n"+
		"Direction:     %s\n\n"+
		"🔗 https://www.binance.com/en/trade/%s_USDT",
		coin, dirLabel,
		coin,
		formatNum(price),
		formatNum(targetPrice),
		strings.ToUpper(direction),
		coin)

	if err := e.alertFn(chatID, msg); err != nil {
		metrics.AlertsFailedTotal.WithLabelValues("binance", "binance_price_alert").Inc()
		e.logger.Error("send binance price alert failed", "chat_id", chatID, "error", err)
	} else {
		metrics.AlertsSentTotal.WithLabelValues("binance", "binance_price_alert").Inc()
		e.logNotification(chatID, "binance_price", "general_binance_price_alert",
			fmt.Sprintf("%s/USDT %s to $%s (current: $%s)", coin, direction, formatNum(targetPrice), formatNum(price)))
	}
}

func formatMerklTVL(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.1fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.0fK", v/1_000)
	}
	return fmt.Sprintf("%.0f", v)
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
		metrics.AlertsFailedTotal.WithLabelValues(src.Name(), "metric_alert").Inc()
		e.logger.Error("send alert failed", "chat_id", chatID, "error", err)
	} else {
		metrics.AlertsSentTotal.WithLabelValues(src.Name(), "metric_alert").Inc()
		e.logNotification(chatID, "metric", src.Name()+"_metric_alert",
			fmt.Sprintf("%s %s %.1f%% in %dm (prev: %s, curr: %s)", metric, direction, changePct*100, windowMin, formatNum(prevVal), formatNum(currVal)))
	}
}

func (e *Engine) sendValueAlert(chatID int64, src Source, metric string, currVal, thresholdVal float64, direction string) {
	dirLabel := "ABOVE"
	cmp := "&gt;"
	if direction == "lower" {
		dirLabel = "BELOW"
		cmp = "&lt;"
	}
	msg := fmt.Sprintf("🚨 %s %s %s THRESHOLD\n\n"+
		"%s is now %s %s %.0f!\n"+
		"Current: %.0f\n"+
		"Threshold: %.0f\n\n"+
		"🔗 %s",
		stringToUpper(src.Name()),
		stringToUpper(metric),
		dirLabel,
		stringToUpper(metric),
		cmp,
		dirLabel,
		thresholdVal,
		currVal,
		thresholdVal,
		src.URL())

	if err := e.alertFn(chatID, msg); err != nil {
		metrics.AlertsFailedTotal.WithLabelValues(src.Name(), "value_alert").Inc()
		e.logger.Error("send alert failed", "chat_id", chatID, "error", err)
	} else {
		metrics.AlertsSentTotal.WithLabelValues(src.Name(), "value_alert").Inc()
		e.logNotification(chatID, "value", src.Name()+"_metric_alert",
			fmt.Sprintf("%s %s %s %.0f (current: %.0f)", metric, direction, dirLabel, thresholdVal, currVal))
	}
}

func (e *Engine) sendDueReports(ctx context.Context, hour int) {
	utc8 := time.FixedZone("UTC+8", 8*60*60)
	today := time.Now().In(utc8).Format("2006-01-02")

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

		sent := 0
		for _, chatID := range chatIDs {
			dedupKey := fmt.Sprintf("report:%s:%d:%s", today, chatID, name)
			if e.dedup.AlreadySent(ctx, dedupKey) {
				metrics.AlertsDeduplicatedTotal.WithLabelValues(name, "daily_report").Inc()
				continue
			}
			if err := e.alertFn(chatID, report); err != nil {
				metrics.AlertsFailedTotal.WithLabelValues(name, "daily_report").Inc()
				e.logger.Error("send alert failed", "chat_id", chatID, "error", err)
				continue
			}
			metrics.AlertsSentTotal.WithLabelValues(name, "daily_report").Inc()
			e.logNotification(chatID, "daily_report", name+"_daily_report",
				fmt.Sprintf("Daily %s report (hour %d UTC+8)", name, hour))
			e.dedup.Record(ctx, dedupKey)
			sent++
		}
		if sent > 0 {
			e.logger.Info("sent daily reports", "source", name, "hour", hour, "recipients", sent)
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

// logNotification persists a notification record for debugging and audit trail.
func (e *Engine) logNotification(chatID int64, alertType, eventName, summary string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.store.LogNotification(ctx, chatID, alertType, eventName, summary); err != nil {
		e.logger.Error("log notification failed", "chat_id", chatID, "alert_type", alertType, "error", err)
	}
	e.logger.Info("notification sent", "chat_id", chatID, "alert_type", alertType, "event", eventName, "summary", summary)
}
