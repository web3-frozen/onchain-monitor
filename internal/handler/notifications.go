package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/web3-frozen/onchain-monitor/internal/store"
)

func ListNotifications(s *store.Store) http.HandlerFunc {
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

		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 100 {
				limit = l
			}
		}

		logs, err := s.ListNotifications(r.Context(), tgChatID, limit)
		if err != nil {
			http.Error(w, `{"error":"failed to list notifications"}`, http.StatusInternalServerError)
			return
		}
		if logs == nil {
			logs = []store.NotificationLog{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(logs)
	}
}
