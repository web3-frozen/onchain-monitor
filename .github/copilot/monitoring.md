# Adding Monitoring to Applications

This skill covers how to add Prometheus metrics, Grafana dashboards, PrometheusRules, and AlertManager alerts to any Go application deployed on the homelab k3s cluster.

## Architecture Overview

```
Go App ──/metrics──▶ Prometheus (kube-prometheus-stack)
                          │
                          ├──▶ Grafana dashboards (ConfigMap sidecar)
                          ├──▶ PrometheusRule alerting
                          └──▶ AlertManager → Telegram
```

### Stack
- **Prometheus**: kube-prometheus-stack v65.8.1, 15d retention, 50Gi storage
- **Grafana**: Sidecar auto-provisions dashboards from ConfigMaps with `grafana_dashboard: "1"` label
- **Loki + Promtail**: Log aggregation, query via `{namespace="<ns>"} | json`
- **AlertManager**: Routes alerts to Telegram via `AlertmanagerConfig` CRD
- **Secrets**: SOPS-encrypted in `pulumi-k3s-proxmox/secrets/secrets.enc.yaml`, synced to K8s via ExternalSecret + Infisical

### Key Configuration
- `serviceMonitorSelectorNilUsesHelmValues: False` → Prometheus discovers ALL ServiceMonitors
- `ruleSelectorNilUsesHelmValues: False` → Prometheus discovers ALL PrometheusRules
- ArgoCD app `monitoring-config` syncs `platform/monitoring/` into `monitoring` namespace
- Grafana datasources: `prometheus` (default), `Loki`

## Step-by-Step: Add Monitoring to a Go Application

### 1. Add prometheus/client_golang dependency

```bash
go get github.com/prometheus/client_golang
```

### 2. Create metrics package

Create `internal/metrics/metrics.go`:

```go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // HTTP RED metrics
    HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Namespace: "<app_name>",
        Subsystem: "http",
        Name:      "requests_total",
        Help:      "Total HTTP requests.",
    }, []string{"method", "path", "status_code"})

    HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Namespace: "<app_name>",
        Subsystem: "http",
        Name:      "request_duration_seconds",
        Help:      "HTTP request latency.",
        Buckets:   prometheus.DefBuckets,
    }, []string{"method", "path"})

    HTTPRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
        Namespace: "<app_name>",
        Subsystem: "http",
        Name:      "requests_in_flight",
        Help:      "Current active requests.",
    })

    // Add custom business metrics as needed
)
```

**Naming convention**: `<app_name>_<subsystem>_<metric_name>` (e.g., `onchain_monitor_poll_total`)

### 3. Create HTTP metrics middleware

Create `internal/middleware/metrics.go`:

```go
package middleware

import (
    "net/http"
    "strconv"
    "time"

    "github.com/go-chi/chi/v5"
    "<module>/internal/metrics"
)

func Metrics() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            metrics.HTTPRequestsInFlight.Inc()
            defer metrics.HTTPRequestsInFlight.Dec()

            ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
            next.ServeHTTP(ww, r)

            routePattern := chi.RouteContext(r.Context()).RoutePattern()
            if routePattern == "" {
                routePattern = "unknown"
            }

            metrics.HTTPRequestsTotal.WithLabelValues(r.Method, routePattern, strconv.Itoa(ww.status)).Inc()
            metrics.HTTPRequestDuration.WithLabelValues(r.Method, routePattern).Observe(time.Since(start).Seconds())
        })
    }
}

type statusRecorder struct {
    http.ResponseWriter
    status int
}

func (sr *statusRecorder) WriteHeader(code int) {
    sr.status = code
    sr.ResponseWriter.WriteHeader(code)
}
```

### 4. Wire into main.go

```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

// Add middleware BEFORE CORS
r.Use(middleware.Metrics())

// Add /metrics endpoint (outside /api prefix)
r.Handle("/metrics", promhttp.Handler())
```

**Important**: Place `middleware.Metrics()` before other middleware to capture all requests. The `/metrics` endpoint is automatically excluded from CORS since it's not under `/api`.

### 5. Instrument business logic

Pattern for timing operations:

```go
start := time.Now()
result, err := doOperation()
duration := time.Since(start).Seconds()
metrics.OperationDuration.WithLabelValues(labels...).Observe(duration)

if err != nil {
    metrics.OperationTotal.WithLabelValues(name, "error").Inc()
} else {
    metrics.OperationTotal.WithLabelValues(name, "success").Inc()
}
```

Pattern for current-state gauges:

```go
metrics.SomeGauge.WithLabelValues(labels...).Set(float64(value))
```

---

## Step-by-Step: Add Kubernetes Monitoring Resources

### 6. ServiceMonitor

Create `homelab-apps/workloads/<app>/servicemonitor.yaml`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: <app>
  namespace: <namespace>
  labels:
    app: <app>
spec:
  selector:
    matchLabels:
      app: <app>
  endpoints:
    - port: http          # must match Service port name
      path: /metrics
      interval: 15s
```

Add to `kustomization.yaml`:
```yaml
resources:
  - servicemonitor.yaml
```

### 7. PrometheusRules

Create `homelab-apps/workloads/<app>/prometheusrules.yaml`:

**Standard rules to include for every app**:

| Alert | Expression | For | Severity |
|-------|-----------|-----|----------|
| `<App>Down` | `up{job="<app>"} == 0` | 2m | critical |
| `<App>HighErrorRate` | `5xx rate / total rate > 0.05` | 5m | warning |
| `<App>HighLatency` | `p95 > 2s` | 5m | warning |

Add app-specific rules for business logic (e.g., poll failures, stale data).

### 8. AlertManager → Telegram

**Prerequisites**:
1. Create a Telegram bot via @BotFather
2. Get the chat_id by messaging the bot and calling `https://api.telegram.org/bot<TOKEN>/getUpdates`
3. Add bot token to Infisical as `<APP>_TELEGRAM_BOT_TOKEN`

**Create ExternalSecret** in `platform/monitoring/`:

```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: alertmanager-<app>-telegram
  namespace: monitoring
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: infisical
    kind: ClusterSecretStore
  target:
    name: alertmanager-<app>-telegram
    creationPolicy: Owner
  data:
    - secretKey: token
      remoteRef:
        key: <APP>_TELEGRAM_BOT_TOKEN
```

**Create AlertManagerConfig** in `platform/monitoring/`:

```yaml
apiVersion: monitoring.coreos.com/v1alpha1
kind: AlertmanagerConfig
metadata:
  name: <app>-telegram
  namespace: monitoring
spec:
  route:
    receiver: telegram
    groupBy: ["alertname"]
    matchers:
      - name: namespace
        value: <namespace>
        matchType: "="
  receivers:
    - name: telegram
      telegramConfigs:
        - chatID: <CHAT_ID>
          botToken:
            name: alertmanager-<app>-telegram
            key: token
```

---

## Step-by-Step: Add Grafana Dashboards

### 9. Dashboard ConfigMaps

Create `platform/monitoring/grafana-dashboard-<app>-<name>.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboard-<app>-<name>
  namespace: monitoring
  labels:
    grafana_dashboard: "1"           # Required for sidecar discovery
  annotations:
    grafana_folder: "<App Name>"     # Groups dashboards in Grafana folder
data:
  <name>.json: |
    {
      "title": "<App> — <Dashboard Name>",
      "uid": "<app>-<name>",
      "schemaVersion": 39,
      "tags": ["<app>"],
      "panels": [ ... ]
    }
```

**Standard dashboards to create**:

1. **Overview**: Uptime, request rate, latency percentiles, error rate, pod CPU/memory, restarts
2. **Business**: App-specific metrics (TVL, prices, counts, etc.)
3. **Logs**: Log volume by level, error stream, all-logs table (Loki datasource)

**Panel PromQL patterns**:

| Metric | PromQL |
|--------|--------|
| Request rate by status | `sum(rate(<app>_http_requests_total[5m])) by (status_code)` |
| p95 latency | `histogram_quantile(0.95, sum(rate(<app>_http_request_duration_seconds_bucket[5m])) by (le))` |
| Error rate | `sum(rate(..{status_code=~"5.."}[5m])) / sum(rate(..[5m]))` |
| Pod CPU | `sum(rate(container_cpu_usage_seconds_total{namespace="<ns>", container="<app>"}[5m]))` |
| Pod memory | `sum(container_memory_working_set_bytes{namespace="<ns>", container="<app>"})`|
| Loki log volume | `sum(count_over_time({namespace="<ns>"} \| json \| level="error" [1m]))` |

**Important**: Use Loki datasource UID `loki` for log panels: `"datasource": { "type": "loki", "uid": "loki" }`

---

## Secrets Management (SOPS)

Bot tokens and chat IDs go in `pulumi-k3s-proxmox/secrets/secrets.enc.yaml`:

```yaml
app_secrets:
  <APP>_TELEGRAM_BOT:
    token: <bot_token>
    chat_id: "<chat_id>"
```

**Encrypted fields** are controlled by `.sops.yaml` `encrypted_regex`. Current pattern:
```
^(proxmox_password|...|token|chat_id|client_id|client_secret|project_id|tunnel_token)$
```

To encrypt/decrypt:
```bash
export SOPS_AGE_KEY_FILE=~/.config/sops/age/keys.txt
AGE_KEY=$(grep 'public key:' $SOPS_AGE_KEY_FILE | awk '{print $NF}')

# Decrypt
sops -d --config <(echo "creation_rules:
  - path_regex: secrets\\.enc\\.yaml\$
    age: \"$AGE_KEY\"
    encrypted_regex: \"<regex>\"") secrets.enc.yaml > secrets.dec.yaml

# Edit secrets.dec.yaml, then re-encrypt
cp secrets.dec.yaml secrets.enc.yaml
sops -e -i --config <(same config) secrets.enc.yaml
rm secrets.dec.yaml
```

---

## File Locations Summary

| What | Where |
|------|-------|
| Go metrics package | `<app>/internal/metrics/metrics.go` |
| HTTP middleware | `<app>/internal/middleware/metrics.go` |
| ServiceMonitor | `homelab-apps/workloads/<app>/servicemonitor.yaml` |
| PrometheusRules | `homelab-apps/workloads/<app>/prometheusrules.yaml` |
| AlertManagerConfig | `homelab-apps/platform/monitoring/alertmanager-config.yaml` |
| ExternalSecret (bot token) | `homelab-apps/platform/monitoring/alertmanager-<app>-externalsecret.yaml` |
| Grafana dashboards | `homelab-apps/platform/monitoring/grafana-dashboard-<app>-*.yaml` |
| SOPS secrets | `pulumi-k3s-proxmox/secrets/secrets.enc.yaml` |
| Infisical ClusterSecretStore | `homelab-apps/platform/external-secrets/cluster-secret-store.yaml` |

## Reference: Onchain Monitor Metrics

The onchain-monitor exposes these Prometheus metrics as a reference implementation:

- `onchain_monitor_http_requests_total` (counter) — method, path, status_code
- `onchain_monitor_http_request_duration_seconds` (histogram) — method, path
- `onchain_monitor_http_requests_in_flight` (gauge)
- `onchain_monitor_poll_total` (counter) — source, status
- `onchain_monitor_poll_duration_seconds` (histogram) — source
- `onchain_monitor_poll_last_success_timestamp` (gauge) — source
- `onchain_monitor_snapshot_count` (gauge) — source
- `onchain_monitor_snapshot_age_seconds` (gauge) — source
- `onchain_monitor_alerts_sent_total` (counter) — source, type
- `onchain_monitor_alerts_failed_total` (counter) — source, type
- `onchain_monitor_alerts_deduplicated_total` (counter) — source, type
- `onchain_monitor_business_metric_value` (gauge) — source, metric_name
- `onchain_monitor_business_subscriptions_active` (gauge) — event_name
- `onchain_monitor_business_telegram_linked_users` (gauge)
- Go runtime: goroutines, GC, memory, file descriptors (default collectors)
