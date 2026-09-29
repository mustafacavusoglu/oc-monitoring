package registry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCheckUsesGETAndOnlyHTTP200MeansPresent(t *testing.T) {
	var requests atomic.Int64
	checker, err := NewChecker("https://nexus.example.test/repository/docker-hosted", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	checker.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			return nil, fmt.Errorf("method = %s, want GET", r.Method)
		}
		status := http.StatusNotFound
		if r.URL.Path == "/repository/docker-hosted/mlops/bch-payments:exists" {
			status = http.StatusOK
		} else if r.URL.Path != "/repository/docker-hosted/mlops/bch-payments:absent" {
			return nil, fmt.Errorf("unexpected request path: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})
	refs := []string{"mlops/bch-payments:exists", "mlops/bch-payments:absent"}
	results := checker.Check(context.Background(), refs)
	if results[refs[0]].Status != "present" || results[refs[1]].Status != "missing" {
		t.Fatalf("results = %#v", results)
	}
	requestsAfterFirstCheck := requests.Load()
	checker.Check(context.Background(), refs)
	if requests.Load() != requestsAfterFirstCheck {
		t.Fatal("cached image IDs triggered additional requests")
	}
}
