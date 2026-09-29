package registry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCheckLogsRequestURLAndResult(t *testing.T) {
	var logs bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	checker, err := NewChecker("https://nexus.example.test/repository/company-private/v2/mlops/", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	checker.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})

	checker.Check(context.Background(), []string{"bch-payments/manifests/absent"})

	output := logs.String()
	if !strings.Contains(output, "https://nexus.example.test/repository/company-private/v2/mlops/bch-payments/manifests/absent") {
		t.Fatalf("logs do not include request URL: %q", output)
	}
	if !strings.Contains(output, "missing") {
		t.Fatalf("logs do not include request result: %q", output)
	}
}

func TestCheckLogsCachedRequestURLAndResult(t *testing.T) {
	var logs bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

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
	if !strings.Contains(output, "cache") {
		t.Fatalf("cached check logs do not identify cache use: %q", output)
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
