package sources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassifyFng(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{10, "😱 Extreme Fear"},
		{25, "😱 Extreme Fear"},
		{26, "😰 Fear"},
		{45, "😰 Fear"},
		{46, "😐 Neutral"},
		{55, "😐 Neutral"},
		{56, "😀 Greed"},
		{75, "😀 Greed"},
		{76, "🤑 Extreme Greed"},
		{100, "🤑 Extreme Greed"},
		{0, "😱 Extreme Fear"},
	}
	for _, tt := range tests {
		got := classifyFng(tt.value)
		if got != tt.want {
			t.Errorf("classifyFng(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestFearGreedFetchSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := fngResponse{Data: []struct {
			Value               string `json:"value"`
			ValueClassification string `json:"value_classification"`
		}{{Value: "42", ValueClassification: "Fear"}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	f := &FearGreed{client: srv.Client(), baseURL: srv.URL}
	snap, err := f.FetchSnapshot()
	if err != nil {
		t.Fatalf("FetchSnapshot error: %v", err)
	}
	if snap.Source != "general" {
		t.Errorf("Source = %q, want %q", snap.Source, "general")
	}
	if snap.Metrics["fear_greed_index"] != 42 {
		t.Errorf("fear_greed_index = %v, want 42", snap.Metrics["fear_greed_index"])
	}
}

func TestFearGreedFetchSnapshotEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := fngResponse{Data: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	f := &FearGreed{client: srv.Client(), baseURL: srv.URL}
	_, err := f.FetchSnapshot()
	if err == nil {
		t.Error("expected error for empty data, got nil")
	}
}
