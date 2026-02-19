# Onchain Monitor — AI Agent Instructions

This file provides context for AI coding agents (GitHub Copilot, etc.) working on this codebase.

## Project Overview

**Onchain Monitor** is a Go-based on-chain metrics monitoring backend. It polls multiple DeFi data sources, collects real-time liquidation data from Binance WebSocket, discovers yield opportunities via Merkl, sends configurable Telegram alerts, and delivers daily reports.

## Architecture

```
cmd/server/main.go          → Entry point, wires everything, errgroup lifecycle
internal/
  collector/                 → Binance Futures WebSocket client (liquidation events)
  config/                    → Environment + Infisical config loading
  dedup/                     → Redis-backed alert deduplication (permanent, fail-closed)
  handler/                   → HTTP handlers (REST API via chi router)
  metrics/                   → Prometheus metric definitions
  middleware/                → CORS, logging, panic recovery, HTTP metrics
  monitor/
    engine.go                → Core polling loop, alert evaluation, daily reports
    source.go                → Source interface + Snapshot model
    sources/                 → Pluggable data sources (one file per source)
  store/                     → PostgreSQL data layer (pgx)
  telegram/                  → Telegram bot (long-polling, OTP linking)
```

## Key Interfaces

### Source Interface (internal/monitor/source.go)
Every data source implements this interface:
```go
type Source interface {
    Name() string                        // Unique identifier (e.g., "altura")
    Chain() string                       // Blockchain name (e.g., "Hyperliquid")
    FetchSnapshot() (*Snapshot, error)   // Current metrics
    FetchDailyReport() (string, error)   // Daily summary text
    URL() string                         // Link to source stats page
}
```

### Adding a new source:
1. Create `internal/monitor/sources/<name>.go`
2. Implement `Source` interface
3. Add a `baseURL` field for httptest testability
4. Register in `cmd/server/main.go`
5. Seed event in `internal/store/migrations.go`
6. Write tests in `internal/monitor/sources/<name>_test.go`

## Alert System

- **Engine** (`engine.go`) polls all sources every 60 seconds
- Each poll compares current metrics against subscriber thresholds
- Alert types: value_alert, metric_alert, maxpain, merkl, binance_price, daily_report
- **Dedup** is permanent (no TTL) and fail-closed (suppresses on Redis failure)
- Dedup keys are cleared when the alert condition resets

## Testing Patterns

- **Pure functions**: Table-driven tests (see `engine_test.go`)
- **HTTP sources**: Use `httptest.NewServer` with mock responses + `baseURL` field
- **Redis dedup**: Use `github.com/alicebob/miniredis/v2` for in-memory Redis
- **Handlers**: Use `httptest.NewRequest` + `httptest.NewRecorder`
- **Engine integration**: Use mock `Source` implementations

## Tech Stack

- **Language**: Go 1.24
- **Router**: chi/v5
- **Database**: PostgreSQL (pgx/v5)
- **Cache**: Redis (go-redis/v9, dedup only)
- **WebSocket**: coder/websocket
- **Metrics**: Prometheus client_golang
- **Secrets**: Infisical (optional)
- **CI**: GitHub Actions (lint, test, build, Docker push to GHCR, Trivy scan)
- **Deploy**: Kubernetes (ArgoCD GitOps)

## Common Commands

```bash
make test       # Run all tests with race detector
make lint       # Run golangci-lint
make build      # Build binary
make docker     # Build Docker image
make run        # Run locally
```

## Important Design Decisions

1. **Fail-closed dedup**: If Redis is down, `AlreadySent()` returns `true` to suppress alerts
2. **errgroup lifecycle**: All goroutines tracked via errgroup for graceful shutdown
3. **30s poll timeout**: Each source poll has a deadline to prevent one slow source from blocking all
4. **Permanent dedup keys**: No TTL; keys cleared only when condition resets or user unsubscribes
5. **Source interface has no context**: `FetchSnapshot()` doesn't take `context.Context`; timeout is enforced externally via goroutine+channel pattern in `fetchWithTimeout()`
