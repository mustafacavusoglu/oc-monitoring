package registry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"monitor/internal/model"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCheckLogsRequestURLAndResult(t *testing.T) {
	var logs bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	checker, err := NewChecker("https://nexus.example.test/repository/company-private/v2/mlops/", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	checker.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})

	checker.Check(context.Background(), []string{"bch-payments/manifests/absent"})

	output := logs.String()
	if !strings.Contains(output, "level=INFO") || !strings.Contains(output, "msg=\"registry image check request\"") || !strings.Contains(output, "method=GET url=") {
		t.Fatalf("logs do not mark the GET request as INFO: %q", output)
	}
	if !strings.Contains(output, "https://nexus.example.test/repository/company-private/v2/mlops/bch-payments/manifests/absent") {
		t.Fatalf("logs do not include request URL: %q", output)
	}
	if !strings.Contains(output, "missing") {
		t.Fatalf("logs do not include request result: %q", output)
	}
}

func TestLookupLogsImageCheckCycleAtInfoLevel(t *testing.T) {
	var logs bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	checker, err := NewChecker("https://nexus.example.test/repository/mlops", 24*time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	ref := "bch-payments/manifests/v1"
	checker.cache[ref] = cacheEntry{
		result:    model.ImageResult{Reference: ref, Status: "exist"},
		expiresAt: time.Now().Add(time.Hour),
	}

	checker.Lookup(context.Background(), []string{ref})

	want := "level=INFO msg=\"registry image check cycle\" references=1 started=0 cache_hits=1 in_flight=0"
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("logs do not include INFO cycle summary %q: %q", want, logs.String())
	}
}

func TestLookupStartsGETAndReusesDailyCache(t *testing.T) {
	var logs bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	checker, err := NewChecker("https://nexus.example.test/repository/mlops", 24*time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	checker.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})
	ref := "bch-payments/manifests/absent"
	first := checker.Lookup(context.Background(), []string{ref})
	if first[ref].Status != "checking" {
		t.Fatalf("first lookup status = %q, want checking", first[ref].Status)
	}
	checker.Check(context.Background(), []string{ref})
	second := checker.Lookup(context.Background(), []string{ref})
	if second[ref].Status != "missing" || requests.Load() != 1 {
		t.Fatalf("cached status = %q, GET requests = %d; want missing, 1", second[ref].Status, requests.Load())
	}
	output := logs.String()
	if !strings.Contains(output, "level=INFO msg=\"registry image check cycle\" references=1 started=1") ||
		!strings.Contains(output, "level=INFO msg=\"registry image check request\" method=GET url=https://nexus.example.test/repository/mlops/bch-payments/manifests/absent") ||
		!strings.Contains(output, "level=INFO msg=\"registry image check cycle\" references=1 started=0 cache_hits=1") {
		t.Fatalf("lookup logs do not show request and cache use: %q", output)
	}
}

func TestMissingImagesUseConfiguredCacheTTL(t *testing.T) {
	checker, err := NewChecker("https://nexus.example.test/repository/mlops", 24*time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	checker.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})

	checker.Check(context.Background(), []string{"bch-payments/manifests/absent"})

	checker.mu.Lock()
	entry := checker.cache["bch-payments/manifests/absent"]
	checker.mu.Unlock()
	remaining := time.Until(entry.expiresAt)
	if remaining < 23*time.Hour || remaining > 24*time.Hour {
		t.Fatalf("missing image cache TTL remaining = %s, want approximately 24h", remaining)
	}
}

func TestCheckLogsCachedRequestURLAndResult(t *testing.T) {
	var logs bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	checker, err := NewChecker("https://repomaster.company.com/repository/company-private/v2/mlops/", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	checker.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})

	refs := []string{"bch-payments/manifests/absent"}
	checker.Check(context.Background(), refs)
	logs.Reset()
	checker.Check(context.Background(), refs)

	output := logs.String()
	if !strings.Contains(output, "https://repomaster.company.com/repository/company-private/v2/mlops/bch-payments/manifests/absent") {
		t.Fatalf("cached check logs do not include request URL: %q", output)
	}
	if !strings.Contains(output, "level=INFO msg=\"registry image check cache hit\"") {
		t.Fatalf("cached check logs do not identify cache use at INFO level: %q", output)
	}
}

func TestCheckUsesGETAndJoinsImageToBasePath(t *testing.T) {
	var requests atomic.Int64
	checker, err := NewChecker("https://nexus.example.test/repository/company-private/v2/mlops/", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	checker.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			return nil, fmt.Errorf("method = %s, want GET", r.Method)
		}
		status := http.StatusNotFound
		if r.URL.Path == "/repository/company-private/v2/mlops/bch-payments/manifests/exists" {
			status = http.StatusOK
		} else if r.URL.Path != "/repository/company-private/v2/mlops/bch-payments/manifests/absent" {
			return nil, fmt.Errorf("unexpected request path: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})
	refs := []string{"bch-payments/manifests/exists", "bch-payments/manifests/absent"}
	results := checker.Check(context.Background(), refs)
	if results[refs[0]].Status != "exist" || results[refs[1]].Status != "missing" {
		t.Fatalf("results = %#v", results)
	}
	requestsAfterFirstCheck := requests.Load()
	checker.Check(context.Background(), refs)
	if requests.Load() != requestsAfterFirstCheck {
		t.Fatal("cached image IDs triggered additional requests")
	}
}
