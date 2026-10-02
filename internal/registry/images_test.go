package registry

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

const template = "https://nexus.example.test/repository/company-private/v2/mlops/bch-{namespace}/manifests/{imageId}"

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &logs
}

func fakeRegistry(checker *Checker, requests *atomic.Int64) {
	checker.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		status := http.StatusNotFound
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/manifests/exists") {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})
}

// waitForCache blocks until the background refresh of url has finished.
func waitForCache(t *testing.T, checker *Checker, url string) cacheEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		checker.mu.Lock()
		entry, ok := checker.cache[url]
		checker.mu.Unlock()
		if ok {
			return entry
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no cached result for %s", url)
	return cacheEntry{}
}

func TestManifestURLFillsTemplate(t *testing.T) {
	checker := NewChecker(template, time.Hour, 1, time.Second)
	got := checker.ManifestURL("yazi-girisi-model", "ald7383")
	want := "https://nexus.example.test/repository/company-private/v2/mlops/bch-yazi-girisi-model/manifests/ald7383"
	if got != want {
		t.Fatalf("ManifestURL = %q, want %q", got, want)
	}
}

func TestLookupChecksInBackgroundAndReusesCache(t *testing.T) {
	logs := captureLogs(t)
	checker := NewChecker(template, 24*time.Hour, 1, time.Second)
	var requests atomic.Int64
	fakeRegistry(checker, &requests)
	exists, absent := checker.ManifestURL("payments", "exists"), checker.ManifestURL("payments", "absent")

	first := checker.Lookup(context.Background(), []string{exists, absent, exists})
	if first[exists].Status != "checking" || first[absent].Status != "checking" {
		t.Fatalf("first lookup = %#v, want checking", first)
	}
	waitForCache(t, checker, exists)
	entry := waitForCache(t, checker, absent)
	if remaining := time.Until(entry.expiresAt); remaining < 23*time.Hour {
		t.Fatalf("cache TTL remaining = %s, want about 24h", remaining)
	}

	second := checker.Lookup(context.Background(), []string{exists, absent})
	if second[exists].Status != "exist" || second[absent].Status != "missing" || requests.Load() != 2 {
		t.Fatalf("cached = %#v, requests = %d; want exist/missing and 2 GETs", second, requests.Load())
	}
	output := logs.String()
	for _, want := range []string{
		`msg="registry image check cycle" references=2 started=2`,
		`msg="registry image check request" method=GET url=` + absent,
		`msg="registry image check result" url=` + absent + ` status=404 state=missing`,
		`msg="registry image check cycle" references=2 started=0 cache_hits=2`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("logs missing %q:\n%s", want, output)
		}
	}
}
