package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckUsesGETAndOnlyHTTP200MeansPresent(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path == "/exists" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	checker, err := NewChecker(server.URL, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{"exists", "absent"}
	results := checker.Check(context.Background(), refs)
	if results["exists"].Status != "present" || results["absent"].Status != "missing" {
		t.Fatalf("results = %#v", results)
	}
	requestsAfterFirstCheck := requests.Load()
	checker.Check(context.Background(), refs)
	if requests.Load() != requestsAfterFirstCheck {
		t.Fatal("cached image IDs triggered additional requests")
	}
}
