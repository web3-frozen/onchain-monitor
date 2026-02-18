# Onchain Monitor — Architecture & Adding New Events

## Project Overview
Go backend monitoring platform that polls DeFi/crypto data sources, stores snapshots, sends Telegram alerts, and exposes a REST API for a Next.js frontend.

## Tech Stack
- **Language**: Go 1.22+
- **Router**: chi/v5
- **Database**: PostgreSQL via pgx/v5
- **Alerts**: Telegram Bot API
- **Frontend**: Next.js + Tailwind CSS (separate repo: `onchain-monitor-frontend`)

## Directory Structure
```
cmd/server/main.go              # Entry point, registers sources & routes
internal/
  config/config.go              # Env vars (DATABASE_URL, TELEGRAM_BOT_TOKEN, etc.)
  handler/
    link.go                     # POST /api/link, POST /api/unlink
    subscriptions.go            # CRUD for subscriptions
    stats.go                    # GET /api/stats, /api/stats/meta
    events.go                   # GET /api/events
  middleware/                   # CORS, logging, recovery
  monitor/
    source.go                   # Source interface + Snapshot struct
    engine.go                   # Polling loop, alert checking, daily reports
    sources/
      altura.go                 # Altura on Hyperliquid
      neverland.go              # Neverland on Monad
      feargreed.go              # Crypto Fear & Greed Index (General)
  store/
    postgres.go                 # All DB operations
    migrations.go               # Schema + seed data
  telegram/bot.go               # Bot commands (/start, /link)
```

## Source Interface (internal/monitor/source.go)
```go
type Source interface {
    Name() string                    // unique key, e.g. "altura", "neverland", "general"
    Chain() string                   // display chain, e.g. "Hyperliquid", "Monad", "General"
    FetchSnapshot() (*Snapshot, error)
    FetchDailyReport() (string, error)
    URL() string                     // link shown in alerts
}

type Snapshot struct {
    Source      string             `json:"source"`
    Chain       string             `json:"chain"`
    Metrics     map[string]float64 `json:"metrics"`
    DataSources map[string]string  `json:"data_sources"`
    FetchedAt   time.Time          `json:"fetched_at"`
}
```

## Alert Types
1. **Percentage-based** (drop/increase): Alert when metric changes >X% in Y minutes. Uses `threshold_pct`, `window_minutes`, `direction` (drop/increase).
2. **Value-based** (higher/lower): Alert when metric crosses absolute threshold. Uses `threshold_value`, `direction` (higher/lower). 60-min dedup.
3. **Daily report**: Sent at subscriber's chosen hour (UTC+8). Uses `report_hour`.

## Engine Flow (engine.go)
- Polls all sources every 1 minute
- Stores up to 60 snapshots per source in `snapHistory`
- Per-subscriber threshold checking after each poll
- Value alerts: checks `currVal > threshold_value` or `currVal < threshold_value`
- Pct alerts: compares current vs N-minutes-ago snapshot
- Daily reports: checks current UTC+8 hour against subscribers' `report_hour`

## Event Naming Convention
Each source gets exactly 2 events:
- `{source_name}_metric_alert` — for threshold alerts
- `{source_name}_daily_report` — for daily reports

## Database (subscriptions table key columns)
- `threshold_pct` — % change threshold (for drop/increase alerts)
- `window_minutes` — time window for % comparison
- `direction` — "drop", "increase", "higher", or "lower"
- `report_hour` — 0-23 UTC+8 hour for daily reports
- `threshold_value` — absolute value threshold (for higher/lower alerts)

---

## How to Add a New Data Source

### Step 1: Create source file
Create `internal/monitor/sources/{name}.go`:

```go
package sources

import (
    "fmt"
    "net/http"
    "time"
    "github.com/web3-frozen/onchain-monitor/internal/monitor"
)

type MySource struct {
    client *http.Client
}

func NewMySource() *MySource {
    return &MySource{client: &http.Client{Timeout: 15 * time.Second}}
}

func (m *MySource) Name() string  { return "mysource" }       // unique key
func (m *MySource) Chain() string { return "MyChain" }         // or "General"
func (m *MySource) URL() string   { return "https://..." }

func (m *MySource) FetchSnapshot() (*monitor.Snapshot, error) {
    // Fetch data from API(s)
    return &monitor.Snapshot{
        Source: m.Name(),
        Chain:  m.Chain(),
        Metrics: map[string]float64{
            "metric_key": value,
        },
        DataSources: map[string]string{
            "metric_key": "API Name",
        },
        FetchedAt: time.Now(),
    }, nil
}

func (m *MySource) FetchDailyReport() (string, error) {
    snap, err := m.FetchSnapshot()
    if err != nil {
        return "", err
    }
    now := time.Now().Format("2006-01-02")
    msg := fmt.Sprintf("📊 MYSOURCE DAILY REPORT — %s\n\n", now)
    // Format metrics...
    msg += "\n🔗 " + m.URL()
    return msg, nil
}
```

### Step 2: Register in main.go
```go
engine.Register(sources.NewMySource())
```

### Step 3: Add events in migrations.go
Add to the INSERT block:
```sql
('mysource_metric_alert', 'Alert when MySource metrics', 'mysource'),
('mysource_daily_report', 'Daily UTC+8 report — MySource metric1, metric2', 'mysource')
```
Also add matching UPDATE statements for idempotent description updates.

### Step 4: Update frontend chain order (page.tsx)
Add to `chainOrder` array:
```ts
const chainOrder = ["General", "Hyperliquid", "Monad", "MyChain"];
```

Add category color in `EventCard.tsx` and `SubscriptionRow.tsx`:
```ts
const categoryColors = {
    mysource: "bg-orange-900/40 text-orange-400",
    // ...
};
```

Add metric labels in `page.tsx`:
```ts
const labels = {
    metric_key: "My Metric",
    // ...
};
```

Add formatting logic if needed:
```ts
if (key === "metric_key") return `${value.toFixed(0)} / 100`;
```

### Step 5: Push both repos
That's it — no handler or engine changes needed. The engine auto-discovers registered sources.
