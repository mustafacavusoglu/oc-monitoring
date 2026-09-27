package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestClassifyLookupResult(t *testing.T) {
	ref := name.MustParseReference("registry.example.test/team/app:v1")
	checkedAt := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		err        error
		contextErr error
		wantStatus string
		wantError  string
	}{
		{name: "manifest found", wantStatus: "present"},
		{name: "manifest missing", err: &transport.Error{StatusCode: 404}, wantStatus: "missing"},
		{name: "unauthorized is unknown", err: &transport.Error{StatusCode: 401}, wantStatus: "unknown", wantError: "registry returned HTTP 401"},
		{name: "rate limited is unknown", err: &transport.Error{StatusCode: 429}, wantStatus: "unknown", wantError: "registry returned HTTP 429"},
		{name: "network error is safe", err: errors.New("sensitive transport details"), wantStatus: "unknown", wantError: "registry lookup failed"},
		{name: "timeout is unknown", err: context.DeadlineExceeded, contextErr: context.DeadlineExceeded, wantStatus: "unknown", wantError: "registry lookup timed out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLookupResult(ref, tc.err, checkedAt, tc.contextErr)
			if got.Status != tc.wantStatus || got.Error != tc.wantError {
				t.Fatalf("result = status %q error %q, want status %q error %q", got.Status, got.Error, tc.wantStatus, tc.wantError)
			}
			if got.CheckedAt == nil || !got.CheckedAt.Equal(checkedAt) {
				t.Fatalf("checkedAt = %v, want %v", got.CheckedAt, checkedAt)
			}
		})
	}
}

func TestCheckMarksInvalidImageReferencesUnknownWithoutLookup(t *testing.T) {
	checker, err := NewChecker("", time.Minute, 2)
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{"(empty image reference)", "not a valid reference"}
	results := checker.Check(context.Background(), refs)
	for _, ref := range refs {
		result := results[ref]
		if result.Status != "unknown" || result.Error != "invalid image reference" || result.CheckedAt != nil {
			t.Fatalf("result for %q = %#v, want unknown invalid reference without registry check", ref, result)
		}
	}
}

func TestCheckUsesRegistryHeadAndOnly404MeansMissing(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/v2/" {
			w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/manifests/v1") {
			w.Header().Set("Docker-Content-Digest", "sha256:"+strings.Repeat("a", 64))
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	checker, err := NewChecker("", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	checker.transport = server.Client().Transport
	host := strings.TrimPrefix(server.URL, "https://")
	refs := []string{
		fmt.Sprintf("%s/team/app:v1", host),
		fmt.Sprintf("%s/team/app:missing", host),
	}
	results := checker.Check(context.Background(), refs)
	if results[refs[0]].Status != "present" {
		t.Fatalf("present image result = %#v, want present", results[refs[0]])
	}
	if results[refs[1]].Status != "missing" {
		t.Fatalf("missing image result = %#v, want missing", results[refs[1]])
	}
	requestsAfterFirstCheck := requests.Load()
	checker.Check(context.Background(), refs)
	if requests.Load() != requestsAfterFirstCheck {
		t.Fatal("cached image references triggered additional registry requests")
	}
}
