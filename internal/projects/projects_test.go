package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"monitor/internal/config"
)

var testRules = rules{batchKeyword: "bch", customServeType: "CustomServe"}

func TestSourceRefreshSelectsBatchAndCustomServeProjects(t *testing.T) {
	data, err := os.ReadFile("testdata/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	source := NewSource(config.Config{
		AzureRepoURL:        server.URL + "/org/_git/repo",
		AzureRepoBranch:     "main",
		AzureProjectsPath:   "projects.json",
		BatchServingKeyword: "BCH",
		CustomServeType:     "CustomServe",
		UpstreamTimeout:     time.Second,
	}, server.Client())
	source.refresh(context.Background())
	snapshot := source.Snapshot()
	if snapshot.Stale {
		t.Fatalf("snapshot stale: %s", snapshot.Error)
	}
	if got, want := snapshot.BatchNamespaces(), []string{"payments-api", "retail-model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("batch namespaces = %v, want %v", got, want)
	}
	if got, want := snapshot.CustomServeNamespaces(), []string{"retail-model", "platform-tools"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("custom serve namespaces = %v, want %v", got, want)
	}
}

func TestParseProjectsSkipsBadEntriesInsteadOfFailing(t *testing.T) {
	raw := map[string]json.RawMessage{
		"ON_PROJE":     json.RawMessage(`{"serving":["BCH"]}`),
		"STRING_PROJE": json.RawMessage(`{"serving":"bch"}`),
		"CM_PROJE":     json.RawMessage(`{"serving":["CM"]}`),
		"BROKEN":       json.RawMessage(`["not","an","object"]`),
		"dup-proje":    json.RawMessage(`{"serving":["BCH"]}`),
		"DUP_PROJE":    json.RawMessage(`{"serving":["BCH"]}`),
		"CUSTOM_PROJE": json.RawMessage(`{"Type":"customserve"}`),
	}
	projects, skipped := parseProjects(raw, testRules)
	snapshot := Snapshot{Projects: projects}

	if got, want := snapshot.BatchNamespaces(), []string{"dup-proje", "on-proje", "string-proje"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("batch namespaces = %v, want %v", got, want)
	}
	if got, want := snapshot.CustomServeNamespaces(), []string{"custom-proje"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("custom serve namespaces = %v, want %v", got, want)
	}
	if len(projects) != 4 {
		t.Fatalf("projects = %d, want 4 (not CM_PROJE, BROKEN or the duplicate)", len(projects))
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped = %q, want BROKEN and the duplicate namespace", skipped)
	}
}
