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

func TestSourceRefreshTurnsEveryKeyIntoANamespace(t *testing.T) {
	data, err := os.ReadFile("testdata/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	source := NewSource(config.Config{
		AzureRepoURL:      server.URL + "/org/_git/repo",
		AzureRepoBranch:   "main",
		AzureProjectsPath: "projects.json",
		UpstreamTimeout:   time.Second,
	}, server.Client())
	source.refresh(context.Background())
	snapshot := source.Snapshot()
	if snapshot.Stale {
		t.Fatalf("snapshot stale: %s", snapshot.Error)
	}
	if got, want := snapshot.Namespaces(), []string{"payments-api", "retail-model", "platform-tools", "unclassified"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("namespaces = %v, want every project %v", got, want)
	}
}

func TestParseProjectsSkipsDuplicateNamespacesInsteadOfFailing(t *testing.T) {
	raw := map[string]json.RawMessage{
		"YAZI_GIRISI_MODEL": json.RawMessage(`{"Serving":["BCH"]}`),
		"ANY_VALUE":         json.RawMessage(`["not","an","object"]`),
		"dup-proje":         json.RawMessage(`{}`),
		"DUP_PROJE":         json.RawMessage(`{}`),
	}
	projects, skipped := parseProjects(raw)
	if got, want := (Snapshot{Projects: projects}).Namespaces(), []string{"any-value", "dup-proje", "yazi-girisi-model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("namespaces = %v, want %v", got, want)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped = %q, want only the duplicate namespace", skipped)
	}
}
