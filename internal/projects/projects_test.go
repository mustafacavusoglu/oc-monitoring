package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"monitor/internal/config"
)

func TestSourceRefreshPublishesOnlyBCHProjects(t *testing.T) {
	data, err := os.ReadFile("testdata/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()
	source := NewSource(config.Config{
		AzureRepoURL:      server.URL + "/org/_git/repo",
		AzureRepoBranch:   "main",
		AzureProjectsPath: "projects.json",
	}, server.Client())
	source.refresh(context.Background())
	snapshot := source.Snapshot()
	want := []string{"payments-api", "retail-model"}
	if !reflect.DeepEqual(snapshot.Namespaces, want) || snapshot.Stale {
		t.Fatalf("project snapshot = %+v, want BCH namespaces %v", snapshot, want)
	}
}

func TestSelectBCHNamespacesFromProjectFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(data, &projects); err != nil {
		t.Fatal(err)
	}

	got, err := selectBCHNamespaces(projects)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"payments-api", "retail-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected namespaces = %v, want %v", got, want)
	}
}

func TestSelectBCHNamespacesNormalizesProjectKeysWithoutType(t *testing.T) {
	projects := map[string]json.RawMessage{
		"KNAK_PROJE": json.RawMessage(`{"serving":["BCH"]}`),
		"CM_PROJE":   json.RawMessage(`{"serving":["CM"]}`),
	}
	got, err := selectBCHNamespaces(projects)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"knak-proje"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected namespaces = %v, want %v", got, want)
	}
}

func TestSelectBCHNamespacesRejectsMalformedServing(t *testing.T) {
	projects := map[string]json.RawMessage{
		"broken": json.RawMessage(`{"serving":"BCH"}`),
	}
	if _, err := selectBCHNamespaces(projects); err == nil {
		t.Fatal("expected malformed serving value to be rejected")
	}
}
