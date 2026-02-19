package sources

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAlphaFetchSnapshot(t *testing.T) {
	sample := `{"airdrops":[{"token":"JCT","date":"2026-02-19","time":"18:00","points":242,"name":"Alpha Box"},{"token":"ICNT","date":"2026-02-19","time":"18:00","points":242,"name":"Alpha Box"}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sample))
	}))
	defer srv.Close()

	a := &Alpha{client: srv.Client(), baseURL: srv.URL}

	snap, err := a.FetchSnapshot()
	if err != nil {
		t.Fatalf("FetchSnapshot error: %v", err)
	}
	if snap.Source != "alpha" {
		t.Errorf("Source = %q, want %q", snap.Source, "alpha")
	}
	if int(snap.Metrics["airdrops"]) != 2 {
		t.Errorf("airdrops = %v, want 2", snap.Metrics["airdrops"])
	}
	if int(snap.Metrics["top_points"]) != 242 {
		t.Errorf("top_points = %v, want 242", snap.Metrics["top_points"])
	}
}

func TestGetAirdrops(t *testing.T) {
	sample := `{"airdrops":[{"token":"JCT","date":"2026-02-19","time":"18:00","points":242,"name":"Alpha Box"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sample))
	}))
	defer srv.Close()

	a := &Alpha{client: srv.Client(), baseURL: srv.URL}
	_, err := a.FetchSnapshot()
	if err != nil {
		t.Fatalf("FetchSnapshot error: %v", err)
	}

	ads := a.GetAirdrops()
	if len(ads) != 1 {
		t.Fatalf("GetAirdrops len = %d, want 1", len(ads))
	}
	if ads[0].Token != "JCT" {
		t.Errorf("token = %q, want JCT", ads[0].Token)
	}
	if ads[0].Points != 242 {
		t.Errorf("points = %d, want 242", ads[0].Points)
	}
	if ads[0].Name != "Alpha Box" {
		t.Errorf("name = %q, want Alpha Box", ads[0].Name)
	}
}
