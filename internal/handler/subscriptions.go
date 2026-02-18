package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/web3-frozen/onchain-monitor/internal/store"
)

func ListSubscriptions(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tgChatIDStr := r.URL.Query().Get("tg_chat_id")
		if tgChatIDStr == "" {
			http.Error(w, `{"error":"tg_chat_id required"}`, http.StatusBadRequest)
			return
		}

		tgChatID, err := strconv.ParseInt(tgChatIDStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid tg_chat_id"}`, http.StatusBadRequest)
			return
		}

		subs, err := s.ListSubscriptions(r.Context(), tgChatID)
		if err != nil {
			http.Error(w, `{"error":"failed to list subscriptions"}`, http.StatusInternalServerError)
			return
		}
		if subs == nil {
			subs = []store.Subscription{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(subs)
	}
}

func Subscribe(s *store.Store) http.HandlerFunc {
	type request struct {
		TgChatID      int64   `json:"tg_chat_id"`
		EventID       int     `json:"event_id"`
		ThresholdPct  float64 `json:"threshold_pct"`
		WindowMinutes int     `json:"window_minutes"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if req.TgChatID == 0 || req.EventID == 0 {
			http.Error(w, `{"error":"tg_chat_id and event_id required"}`, http.StatusBadRequest)
			return
		}

		// Defaults for non-drop events
		if req.ThresholdPct <= 0 {
			req.ThresholdPct = 10
		}
		if req.WindowMinutes <= 0 {
			req.WindowMinutes = 1
		}

		sub, err := s.Subscribe(r.Context(), req.TgChatID, req.EventID, req.ThresholdPct, req.WindowMinutes)
		if err != nil {
			http.Error(w, `{"error":"failed to subscribe"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(sub)
	}
}

func Unsubscribe(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid subscription id"}`, http.StatusBadRequest)
			return
		}

		if err := s.Unsubscribe(r.Context(), id); err != nil {
			http.Error(w, `{"error":"failed to unsubscribe"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
