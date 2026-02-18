package sources

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
	"github.com/web3-frozen/onchain-monitor/internal/store"
)

// binSize maps symbol to price bin width for max pain calculation.
var binSize = map[string]float64{
	"BTC": 100,
	"ETH": 10,
}

// trackedCoins are the symbols we calculate max pain for.
var trackedCoins = []string{"BTC", "ETH"}

// windowFromInterval converts interval strings to durations.
var windowFromInterval = map[string]time.Duration{
	"12h": 12 * time.Hour,
	"24h": 24 * time.Hour,
	"48h": 48 * time.Hour,
	"3d":  72 * time.Hour,
	"7d":  168 * time.Hour,
}

// MaxPain calculates liquidation max pain from Binance forceOrder data stored in Postgres.
type MaxPain struct {
	logger  *slog.Logger
	store   *store.Store
	mu      sync.RWMutex
	entries map[string]monitor.MaxPainEntry // keyed by "SYMBOL:interval"
}

func NewMaxPain(logger *slog.Logger, db *store.Store) *MaxPain {
	return &MaxPain{
		logger:  logger,
		store:   db,
		entries: make(map[string]monitor.MaxPainEntry),
	}
}

func (m *MaxPain) Name() string  { return "maxpain" }
func (m *MaxPain) Chain() string { return "General" }
func (m *MaxPain) URL() string   { return "https://www.binance.com/en/futures/BTCUSDT" }

// GetEntry returns the latest max pain data for a coin and interval.
func (m *MaxPain) GetEntry(symbol, interval string) (monitor.MaxPainEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := strings.ToUpper(symbol) + ":" + interval
	e, ok := m.entries[key]
	return e, ok
}

// FetchSnapshot queries Postgres for 24h liquidation max pain.
func (m *MaxPain) FetchSnapshot() (*monitor.Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	allEntries := make(map[string]monitor.MaxPainEntry)

	for _, sym := range trackedCoins {
		entry, err := m.queryMaxPain(ctx, sym, "24h")
		if err != nil {
			m.logger.Warn("maxpain query failed", "symbol", sym, "error", err)
			continue
		}
		if entry.Price <= 0 {
			continue
		}
		key := sym + ":24h"
		allEntries[key] = entry
	}

	m.mu.Lock()
	for k, v := range allEntries {
		m.entries[k] = v
	}
	m.mu.Unlock()

	met := make(map[string]float64)
	dataSources := make(map[string]string)
	for _, e := range allEntries {
		sym := strings.ToUpper(e.Symbol)
		met[sym+"_price"] = e.Price
		met[sym+"_long_maxpain"] = e.MaxLongLiquidationPrice
		met[sym+"_short_maxpain"] = e.MaxShortLiquidationPrice
		dataSources[sym+"_price"] = "Binance Futures"
		dataSources[sym+"_long_maxpain"] = "Binance Futures"
		dataSources[sym+"_short_maxpain"] = "Binance Futures"
	}

	return &monitor.Snapshot{
		Source:      "maxpain",
		Chain:       "General",
		Metrics:     met,
		DataSources: dataSources,
		FetchedAt:   time.Now(),
	}, nil
}

// ScrapeInterval queries a specific interval and updates the cache.
func (m *MaxPain) ScrapeInterval(interval string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, sym := range trackedCoins {
		entry, err := m.queryMaxPain(ctx, sym, interval)
		if err != nil {
			return err
		}
		if entry.Price <= 0 {
			continue
		}
		m.mu.Lock()
		m.entries[sym+":"+interval] = entry
		m.mu.Unlock()
	}
	return nil
}

// ScrapeIntervals queries multiple intervals.
func (m *MaxPain) ScrapeIntervals(intervals []string) error {
	for _, iv := range intervals {
		if err := m.ScrapeInterval(iv); err != nil {
			return err
		}
	}
	return nil
}

func (m *MaxPain) FetchDailyReport() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.entries) == 0 {
		return "", fmt.Errorf("no maxpain data available")
	}

	var b strings.Builder
	b.WriteString("📊 Liquidation Max Pain Report (24h)\nData: Binance Futures liquidations\n\n")
	for _, sym := range trackedCoins {
		e, ok := m.entries[sym+":24h"]
		if !ok {
			continue
		}
		longDist := (e.MaxLongLiquidationPrice - e.Price) / e.Price * 100
		shortDist := (e.Price - e.MaxShortLiquidationPrice) / e.Price * 100
		b.WriteString(fmt.Sprintf("%s  $%s\n", sym, fmtNum(e.Price)))
		b.WriteString(fmt.Sprintf("  Long Max Pain:  $%s (%.1f%%)\n", fmtNum(e.MaxLongLiquidationPrice), longDist))
		b.WriteString(fmt.Sprintf("  Short Max Pain: $%s (%.1f%%)\n\n", fmtNum(e.MaxShortLiquidationPrice), shortDist))
	}
	b.WriteString("🔗 https://www.binance.com/en/futures/BTCUSDT")
	return b.String(), nil
}

// queryMaxPain queries Postgres for liquidation max pain for a single symbol+interval.
func (m *MaxPain) queryMaxPain(ctx context.Context, symbol, interval string) (monitor.MaxPainEntry, error) {
	window, ok := windowFromInterval[interval]
	if !ok {
		window = 24 * time.Hour
	}
	bs, ok := binSize[symbol]
	if !ok {
		bs = 100
	}

	price, err := m.store.GetCurrentPrice(ctx, symbol)
	if err != nil {
		return monitor.MaxPainEntry{}, fmt.Errorf("get price for %s: %w", symbol, err)
	}

	longMP, shortMP, err := m.store.QueryMaxPain(ctx, symbol, window, bs)
	if err != nil {
		return monitor.MaxPainEntry{}, fmt.Errorf("query maxpain %s %s: %w", symbol, interval, err)
	}

	return monitor.MaxPainEntry{
		Symbol:                   symbol,
		Price:                    price,
		MaxLongLiquidationPrice:  longMP.PriceBin,
		MaxShortLiquidationPrice: shortMP.PriceBin,
		Interval:                 interval,
	}, nil
}

func fmtNum(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.2fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.2f", math.Round(v*100)/100)
	}
	return fmt.Sprintf("%.4f", v)
}
