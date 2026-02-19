# Onchain Monitor — Backend

A Go-based on-chain metrics monitoring API. Polls multiple DeFi data sources, collects real-time liquidation data from Binance, discovers yield opportunities via Merkl and Turtle, sends configurable Telegram alerts, and delivers daily reports.

## Architecture

```
┌─────────────┐     ┌──────────┐     ┌────────────┐
│  Data Sources│────▶│  Engine  │────▶│  Telegram  │
│  (pluggable) │     │ (poll/   │     │  Alerts &  │
│              │     │  detect) │     │  Reports   │
└─────────────┘     └────┬─────┘     └────────────┘
                         │
┌─────────────┐     ┌────▼─────┐     ┌────────────┐
│  Binance WS │────▶│ PostgreSQL│◀───│  REST API  │
│  (liquidations)   │          │     │  (chi)     │
└─────────────┘     └────┬─────┘     └────────────┘
                         │
                    ┌────▼─────┐
                    │  Redis   │
                    │  (dedup) │
                    └──────────┘
```

## Data Sources

| Source | Metrics | Data Provider |
|--------|---------|---------------|
| **Altura** | TVL, AVLT Price, APR | Altura GraphQL API |
| **Neverland** | TVL, veDUST TVL, DUST Price, Fees (24h/7d/30d) | DefiLlama + DexScreener APIs |
| **Fear & Greed** | Fear & Greed Index (0–100) | alternative.me API |
| **Max Pain** | BTC/ETH Long & Short Max Pain prices | Self-built from Binance Futures WebSocket liquidations |
| **Merkl** | Yield opportunities (APR, TVL, action) | Merkl v4 API (api.merkl.xyz) |
| **Turtle** | Yield opportunities (yield, TVL, type) | Turtle API (api.turtle.xyz) |
| **Binance** | BTC/USDT price (or any symbol) | Binance public ticker API |

### Adding a New Source

Implement the `Source` interface in `internal/monitor/sources/` and register it in `main.go`:

```go
type Source interface {
    Name() string
    FetchSnapshot() (*Snapshot, error)
    FetchDailyReport() (string, error)
    URL() string
}
```

## Alert Types

| Type | Description | Dedup Strategy |
|------|-------------|----------------|
| **Value alerts** | Fires when a metric crosses an absolute threshold (above/below) | Permanent until condition resets |
| **Metric alerts** | Fires on percentage change (increase/drop) over a configurable time window | Permanent until condition resets |
| **Max Pain alerts** | Fires when current price is near liquidation max pain level | Permanent until condition resets |
| **Merkl alerts** | Fires on new yield opportunities matching user's APR/TVL/action/stablecoin criteria | Permanent per opportunity per user |
| **Turtle alerts** | Fires on new Turtle yield opportunities matching user's yield/TVL/type/stablecoin criteria | Permanent per opportunity per user |
| **Binance price alerts** | Fires when a coin's price crosses a user-defined target (increase/decrease to X) | Permanent until condition resets |
| **Daily reports** | Scheduled summary sent at configured hour (UTC+8) | Keyed by date (naturally unique) |

All alerts use **fire-once semantics** — no TTL. Dedup keys are stored permanently in Redis and cleared only when the alert condition resets or the user unsubscribes. Dedup is **fail-closed**: if Redis is unreachable, alerts are suppressed rather than re-fired.

## Resilience

- **Graceful shutdown** — all background goroutines (engine, telegram bot, liquidation collector) are managed via `errgroup`. On SIGINT/SIGTERM the context is cancelled, goroutines drain, and the HTTP server shuts down with a 30 s deadline.
- **Source poll timeout** — each `FetchSnapshot()` call has a 30 s deadline. A single slow or hanging source cannot block the entire poll cycle.
- **Dedup fail-closed** — if Redis is unreachable, `AlreadySent()` returns `true` to suppress duplicate alerts during downtime.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Liveness probe |
| `GET` | `/readyz` | Readiness probe (checks DB) |
| `GET` | `/metrics` | Prometheus metrics endpoint |
| `GET` | `/api/events` | List available monitoring events |
| `GET` | `/api/stats` | Latest snapshots for all sources (or `?source=altura`) |
| `POST` | `/api/link` | Link a Telegram account via OTP code |
| `GET` | `/api/subscriptions` | List user's event subscriptions |
| `POST` | `/api/subscriptions` | Subscribe to an event |
| `DELETE` | `/api/subscriptions/{id}` | Unsubscribe (also clears dedup keys) |

## Monitoring & Observability

### Prometheus Metrics

- **HTTP**: `http_requests_total`, `http_request_duration_seconds`, `http_requests_in_flight`
- **Polling**: `monitor_poll_total`, `monitor_poll_duration_seconds`, `monitor_poll_last_success_timestamp`
- **Alerts**: `monitor_alerts_sent_total`, `monitor_alerts_failed_total`, `monitor_alerts_deduplicated_total`
- **Business**: `monitor_metric_value` (TVL, prices, APR, etc.), `monitor_subscriptions_active`

### Infrastructure Alerts (PrometheusRules → AlertManager → Telegram)

- `OnchainMonitorDown` — API unreachable for >2 min
- `OnchainMonitorHighErrorRate` — HTTP 5xx rate >5%
- `OnchainMonitorPollFailure` — Poll error rate >50% for any source
- `OnchainMonitorPollStale` — No successful poll for >3 min
- `OnchainMonitorHighLatency` — p95 latency >2s
- `OnchainMonitorDBStorageHigh` — PostgreSQL PVC usage >80%

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `TELEGRAM_BOT_TOKEN` | Yes | — | Telegram Bot API token (or via Infisical) |
| `REDIS_URL` | Yes | — | Redis connection string (for alert dedup) |
| `PORT` | No | `8080` | HTTP listen port |
| `FRONTEND_ORIGIN` | No | `*` | CORS allowed origin |
| `INFISICAL_CLIENT_ID` | No | — | Infisical Universal Auth client ID |
| `INFISICAL_CLIENT_SECRET` | No | — | Infisical Universal Auth client secret |
| `INFISICAL_PROJECT_ID` | No | — | Infisical project ID |
| `INFISICAL_SITE_URL` | No | cluster-internal | Infisical API base URL |
| `INFISICAL_ENV` | No | `prod` | Infisical environment slug |

When Infisical credentials are provided, `TELEGRAM_BOT_TOKEN` is fetched from Infisical at startup if not already set via environment.

## Local Development

```bash
# Prerequisites: Go 1.23+, PostgreSQL, Redis

export DATABASE_URL="postgres://user:pass@localhost:5432/onchain_monitor?sslmode=disable"
export TELEGRAM_BOT_TOKEN="your-bot-token"
export REDIS_URL="redis://localhost:6379"

go run ./cmd/server
```

## Docker

```bash
docker build -t onchain-monitor .
docker run -p 8080:8080 \
  -e DATABASE_URL="..." \
  -e TELEGRAM_BOT_TOKEN="..." \
  -e REDIS_URL="..." \
  onchain-monitor
```

## Deployment

Deployed to Kubernetes via ArgoCD GitOps. CI/CD handled by GitHub Actions:

1. Push to `main` triggers lint, test, build, and Docker image push to GHCR
2. Image tag is updated in `homelab-apps` kustomization
3. ArgoCD syncs the new manifest

## Project Structure

```
cmd/server/main.go          # Entry point — wires sources, engine, handlers; errgroup lifecycle
internal/
  collector/
    binance_ws.go           # Binance Futures WebSocket client (forceOrder streams)
    collector.go            # Orchestrator — manages WS connections, writes to Postgres
  config/                   # Environment + Infisical config loading
  dedup/
    dedup.go                # Redis-backed alert deduplication (permanent, no TTL, fail-closed)
  handler/                  # HTTP handlers (events, stats, subscriptions, link)
  metrics/                  # Prometheus metrics registry
  middleware/               # CORS, logging, recovery, metrics
  monitor/
    engine.go               # Core polling loop, alert evaluation, daily reports
    source.go               # Source interface + Snapshot model
    sources/
      altura.go             # Altura data source (GraphQL)
      neverland.go          # Neverland data source (DefiLlama + DexScreener)
      feargreed.go          # Fear & Greed Index (alternative.me)
      maxpain.go            # Max Pain from Binance liquidation data
      merkl.go              # Merkl yield opportunities (API v4)
      turtle.go             # Turtle yield opportunities (api.turtle.xyz)
      binance.go            # Binance price alerts (public ticker API)
  store/                    # PostgreSQL store + migrations
  telegram/                 # Telegram bot (long-polling, OTP linking)
scripts/
  clear-dedup.sh            # Clear Redis dedup keys for a specific chat ID
```

## Scripts

### Clear Alert Dedup Keys

```bash
# List keys for a chat ID (dry run)
./scripts/clear-dedup.sh <chat_id> --dry-run

# Delete all dedup keys for a chat ID
./scripts/clear-dedup.sh <chat_id>
```

## License

This project is licensed under the [MIT License](LICENSE).
