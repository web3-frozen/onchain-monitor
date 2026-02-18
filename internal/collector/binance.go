package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/web3-frozen/onchain-monitor/internal/store"
)

const (
	binanceWSBase = "wss://fstream.binance.com/ws"
	reconnectBase = 2 * time.Second
	reconnectMax  = 60 * time.Second
	flushInterval = 5 * time.Second
	cleanupAge    = 30 * 24 * time.Hour // keep 30 days of data (supports 1M interval)
	cleanupEvery  = 1 * time.Hour
)

// Symbols to track (lowercase for Binance WS stream names).
var trackedSymbols = []string{"btcusdt", "ethusdt"}

// binanceForceOrder is the WebSocket message for a forced liquidation.
type binanceForceOrder struct {
	Event     string `json:"e"`
	EventTime int64  `json:"E"`
	Order     struct {
		Symbol    string `json:"s"`
		Side      string `json:"S"`  // "SELL" = long liquidated, "BUY" = short liquidated
		Quantity  string `json:"q"`
		Price     string `json:"p"`
		AvgPrice  string `json:"ap"`
		Status    string `json:"X"`
		FilledQty string `json:"z"`
		TradeTime int64  `json:"T"`
	} `json:"o"`
}

// Collector manages WebSocket connections to Binance and stores liquidation events.
type Collector struct {
	store  *store.Store
	logger *slog.Logger

	mu     sync.Mutex
	buffer []store.LiquidationEvent
}

// New creates a new liquidation collector.
func New(db *store.Store, logger *slog.Logger) *Collector {
	return &Collector{
		store:  db,
		logger: logger,
		buffer: make([]store.LiquidationEvent, 0, 100),
	}
}

// Run starts the collector. Blocks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	streams := make([]string, len(trackedSymbols))
	for i, s := range trackedSymbols {
		streams[i] = s + "@forceOrder"
	}
	wsURL := binanceWSBase + "/" + strings.Join(streams, "/")

	go c.flushLoop(ctx)
	go c.cleanupLoop(ctx)

	c.logger.Info("liquidation collector starting", "symbols", trackedSymbols, "url", wsURL)

	backoff := reconnectBase
	for {
		select {
		case <-ctx.Done():
			c.flush(context.Background())
			return
		default:
		}

		err := c.connectAndRead(ctx, wsURL)
		if ctx.Err() != nil {
			return
		}

		c.logger.Warn("binance ws disconnected, reconnecting...", "error", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = time.Duration(math.Min(float64(backoff*2), float64(reconnectMax)))
	}
}

func (c *Collector) connectAndRead(ctx context.Context, wsURL string) error {
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.CloseNow() //nolint:errcheck // best-effort close on exit

	c.logger.Info("binance ws connected")

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("ws read: %w", err)
		}
		c.handleMessage(data)
	}
}

func (c *Collector) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.flush(ctx)
		}
	}
}

func (c *Collector) flush(ctx context.Context) {
	c.mu.Lock()
	if len(c.buffer) == 0 {
		c.mu.Unlock()
		return
	}
	events := c.buffer
	c.buffer = make([]store.LiquidationEvent, 0, 100)
	c.mu.Unlock()

	if err := c.store.InsertLiquidationEvents(ctx, events); err != nil {
		c.logger.Error("flush liquidation events failed", "count", len(events), "error", err)
		c.mu.Lock()
		c.buffer = append(events, c.buffer...)
		c.mu.Unlock()
		return
	}
	if len(events) > 0 {
		c.logger.Debug("flushed liquidation events", "count", len(events))
	}
}

func (c *Collector) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := c.store.CleanupOldLiquidationEvents(ctx, cleanupAge)
			if err != nil {
				c.logger.Error("cleanup old liquidation events failed", "error", err)
			} else if deleted > 0 {
				c.logger.Info("cleaned up old liquidation events", "deleted", deleted)
			}
		}
	}
}

func (c *Collector) handleMessage(data []byte) {
	var msg binanceForceOrder
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	if msg.Event != "forceOrder" || msg.Order.Status != "FILLED" {
		return
	}

	price := parseFloat(msg.Order.AvgPrice)
	if price <= 0 {
		price = parseFloat(msg.Order.Price)
	}
	qty := parseFloat(msg.Order.FilledQty)
	if qty <= 0 {
		qty = parseFloat(msg.Order.Quantity)
	}

	var side string
	switch msg.Order.Side {
	case "SELL":
		side = "LONG"
	case "BUY":
		side = "SHORT"
	default:
		return
	}

	symbol := strings.TrimSuffix(strings.ToUpper(msg.Order.Symbol), "USDT")

	c.mu.Lock()
	c.buffer = append(c.buffer, store.LiquidationEvent{
		Symbol:    symbol,
		Side:      side,
		Price:     price,
		Quantity:  qty,
		USDValue:  price * qty,
		Exchange:  "binance",
		EventTime: time.UnixMilli(msg.Order.TradeTime),
	})
	c.mu.Unlock()
}

func parseFloat(s string) float64 {
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}
