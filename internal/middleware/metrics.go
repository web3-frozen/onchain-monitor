package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/web3-frozen/onchain-monitor/internal/metrics"
)

// Metrics returns a chi middleware that records Prometheus HTTP metrics.
func Metrics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			metrics.HTTPRequestsInFlight.Inc()
			defer metrics.HTTPRequestsInFlight.Dec()

			ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(ww, r)

			// Use chi route pattern for low-cardinality path label
			routePattern := chi.RouteContext(r.Context()).RoutePattern()
			if routePattern == "" {
				routePattern = "unknown"
			}

			duration := time.Since(start).Seconds()
			statusCode := strconv.Itoa(ww.status)

			metrics.HTTPRequestsTotal.WithLabelValues(r.Method, routePattern, statusCode).Inc()
			metrics.HTTPRequestDuration.WithLabelValues(r.Method, routePattern).Observe(duration)
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
