package handler

import (
	"encoding/json"
	"net/http"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

func Stats(engine *monitor.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		source := r.URL.Query().Get("source")
		w.Header().Set("Content-Type", "application/json")

		if source != "" {
			snap := engine.GetSnapshot(source)
			if snap == nil {
				http.Error(w, `{"error":"no data available yet"}`, http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(snap)
			return
		}

		// Return all sources
		all := make([]*monitor.Snapshot, 0)
		for _, name := range engine.SourceNames() {
			if snap := engine.GetSnapshot(name); snap != nil {
				all = append(all, snap)
			}
		}
		_ = json.NewEncoder(w).Encode(all)
	}
}
