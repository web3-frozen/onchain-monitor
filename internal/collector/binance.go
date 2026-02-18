package collector

import (
"context"
"encoding/json"
"fmt"
"io"
"log/slog"
"math"
"net/http"
"strings"
"sync"
"time"

"github.com/coder/websocket"
"github.com/web3-frozen/onchain-monitor/internal/store"
)

const (
binanceWSBase  = "wss://fstream.binance.com/ws"
binanceRestURL = "https://fapi.binance.com/fapi/v1/allForceOrders"
reconnectBase  = 2 * time.Second
reconnectMax   = 60 * time.Second
flushInterval  = 5 * time.Second
cleanupAge     = 30 * 24 * time.Hour // keep 30 days (supports 1M interval)
cleanupEvery   = 1 * time.Hour
)

var trackedSymbols = []string{"btcusdt", "ethusdt"}

type binanceForceOrder struct {
Event     string `json:"e"`
EventTime int64  `json:"E"`
Order     struct {
Symbol    string `json:"s"`
Side      string `json:"S"`
Quantity  string `json:"q"`
Price     string `json:"p"`
AvgPrice  string `json:"ap"`
Status    string `json:"X"`
FilledQty string `json:"z"`
TradeTime int64  `json:"T"`
} `json:"o"`
}

type binanceRestOrder struct {
Symbol       string `json:"symbol"`
Price        string `json:"price"`
OrigQty      string `json:"origQty"`
ExecutedQty  string `json:"executedQty"`
AveragePrice string `json:"averagePrice"`
Side         string `json:"side"`
Status       string `json:"status"`
Time         int64  `json:"time"`
}

type Collector struct {
store  *store.Store
logger *slog.Logger
client *http.Client

mu     sync.Mutex
buffer []store.LiquidationEvent
}

func New(db *store.Store, logger *slog.Logger) *Collector {
return &Collector{
store:  db,
logger: logger,
client: &http.Client{Timeout: 15 * time.Second},
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

// Backfill recent liquidations from REST API so maxpain has data immediately.
c.backfill(ctx)

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

// backfill fetches recent liquidation orders from Binance REST API.
func (c *Collector) backfill(ctx context.Context) {
for _, sym := range trackedSymbols {
symbol := strings.ToUpper(sym)
url := fmt.Sprintf("%s?symbol=%s&limit=100", binanceRestURL, symbol)
req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
if err != nil {
c.logger.Warn("backfill request create failed", "symbol", symbol, "error", err)
continue
}
resp, err := c.client.Do(req)
if err != nil {
c.logger.Warn("backfill request failed", "symbol", symbol, "error", err)
continue
}
body, err := io.ReadAll(resp.Body)
resp.Body.Close() //nolint:errcheck
if err != nil || resp.StatusCode != 200 {
c.logger.Warn("backfill read failed", "symbol", symbol, "status", resp.StatusCode)
continue
}

var orders []binanceRestOrder
if err := json.Unmarshal(body, &orders); err != nil {
c.logger.Warn("backfill parse failed", "symbol", symbol, "error", err)
continue
}

var events []store.LiquidationEvent
for _, o := range orders {
ev := restOrderToEvent(o)
if ev != nil {
events = append(events, *ev)
}
}

if len(events) > 0 {
if err := c.store.InsertLiquidationEvents(ctx, events); err != nil {
c.logger.Warn("backfill insert failed", "symbol", symbol, "error", err)
} else {
c.logger.Info("backfilled liquidation events", "symbol", symbol, "count", len(events))
}
}
}
}

func restOrderToEvent(o binanceRestOrder) *store.LiquidationEvent {
price := parseFloat(o.AveragePrice)
if price <= 0 {
price = parseFloat(o.Price)
}
qty := parseFloat(o.ExecutedQty)
if qty <= 0 {
qty = parseFloat(o.OrigQty)
}
if price <= 0 || qty <= 0 {
return nil
}

var side string
switch o.Side {
case "SELL":
side = "LONG"
case "BUY":
side = "SHORT"
default:
return nil
}

symbol := strings.TrimSuffix(o.Symbol, "USDT")
return &store.LiquidationEvent{
Symbol:    symbol,
Side:      side,
Price:     price,
Quantity:  qty,
USDValue:  price * qty,
Exchange:  "binance",
EventTime: time.UnixMilli(o.Time),
}
}

func (c *Collector) connectAndRead(ctx context.Context, wsURL string) error {
conn, _, err := websocket.Dial(ctx, wsURL, nil)
if err != nil {
return fmt.Errorf("ws dial: %w", err)
}
defer conn.CloseNow() //nolint:errcheck

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
if msg.Event != "forceOrder" {
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
if price <= 0 || qty <= 0 {
return
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
