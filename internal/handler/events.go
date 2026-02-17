package handler

import (
	"encoding/json"
	"net/http"

	"github.com/web3-frozen/onchain-monitor/internal/store"
)

func ListEvents(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := s.ListEvents(r.Context())
		if err != nil {
			http.Error(w, `{"error":"failed to list events"}`, http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []store.Event{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(events)
	}
}
