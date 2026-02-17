# Onchain Monitor — Backend

A generic on-chain metrics monitoring API built with Go. It polls multiple DeFi data sources, detects anomalies (≥10% drops), sends Telegram alerts, and delivers daily reports.

## Architecture

```
┌─────────────┐     ┌──────────┐     ┌────────────┐
│  Data Sources│────▶│  Engine  │────▶│  Telegram  │
│  (pluggable) │     │ (poll/   │     │  Alerts &  │
│              │     │  detect) │     │  Reports   │
└─────────────┘     └────┬─────┘     └────────────┘
                         │
                    ┌────▼─────┐     ┌────────────┐
                    │ PostgreSQL│◀───│  REST API  │
                    │          │     │  (chi)     │
                    └──────────┘     └────────────┘
```

## Data Sources

| Source | Metrics | APIs |
|--------|---------|------|
| **Altura** | TVL, AVLT Price, APR | Altura GraphQL |
| **Neverland** | TVL, veDUST TVL, DUST Price, Fees (24h/7d/30d) | DefiLlama, DexScreener |

Adding a new source: implement the `Source` interface in `internal/monitor/sources/` and register it in `main.go`.

```go
type Source interface {
    Name() string
    FetchSnapshot() (*Snapshot, error)
    FetchDailyReport() (string, error)
    URL() string
}
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Liveness probe |
| `GET` | `/readyz` | Readiness probe (checks DB) |
| `GET` | `/api/events` | List available monitoring events |
| `GET` | `/api/stats` | Latest snapshots for all sources (or `?source=altura`) |
| `POST` | `/api/link` | Link a Telegram account via OTP code |
| `GET` | `/api/subscriptions` | List user's event subscriptions |
| `POST` | `/api/subscriptions` | Subscribe to an event |
| `DELETE` | `/api/subscriptions/{id}` | Unsubscribe from an event |

## Monitoring Behaviour

- **Polling**: every 60 seconds per source
- **Drop alerts**: triggered when any metric drops ≥10% from the previous snapshot
- **Daily reports**: sent at 08:00 HKT to all subscribers

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `TELEGRAM_BOT_TOKEN` | Yes | — | Telegram Bot API token (or via Infisical) |
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
# Prerequisites: Go 1.23+, PostgreSQL

export DATABASE_URL="postgres://user:pass@localhost:5432/onchain_monitor?sslmode=disable"
export TELEGRAM_BOT_TOKEN="your-bot-token"

go run ./cmd/server
```

## Docker

```bash
docker build -t onchain-monitor .
docker run -p 8080:8080 \
  -e DATABASE_URL="..." \
  -e TELEGRAM_BOT_TOKEN="..." \
  onchain-monitor
```

## Deployment

The app is deployed to Kubernetes via ArgoCD GitOps. CI/CD is handled by GitHub Actions:

1. Push to `main` triggers lint, test, build, and Docker image push to GHCR
2. Image tag is updated in `homelab-apps` kustomization
3. ArgoCD syncs the new manifest

## Project Structure

```
cmd/server/main.go          # Entry point
internal/
  config/                   # Environment + Infisical config loading
  handler/                  # HTTP handlers (events, stats, subscriptions, link)
  middleware/               # CORS, logging, recovery
  monitor/
    engine.go               # Core polling loop, drop detection, daily reports
    source.go               # Source interface + Snapshot model
    sources/
      altura.go             # Altura data source
      neverland.go          # Neverland data source
  store/                    # PostgreSQL store + migrations
  telegram/                 # Telegram bot (long-polling, OTP linking)
```

## License

Private repository — all rights reserved.
