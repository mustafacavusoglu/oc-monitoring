package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"monitor/internal/cluster"
	"monitor/internal/model"
	"monitor/internal/projects"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDashboardEndpointReturnsSampleSnapshotAndRegistryHealth(t *testing.T) {
	lastSuccess := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	checkedAt := lastSuccess.Add(time.Minute)
	cron := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "daily-job", "namespace": "payments"},
		"spec": map[string]any{
			"schedule": "0 12 * * *",
			"workflowSpec": map[string]any{"templates": []any{
				map[string]any{"name": "run", "container": map[string]any{"image": "registry.example.test/payments/team/app:v1"}},
			}},
		},
	}}
	api := newHandler(
		func() projects.Snapshot {
			return projects.Snapshot{
				Namespaces:      []string{"payments"},
				ImageNamespaces: []string{"payments"},
				LastSuccess:     &lastSuccess,
			}
		},
		func() cluster.Snapshot {
			return cluster.Snapshot{CronWorkflows: []*unstructured.Unstructured{cron}, Health: model.SourceHealth{State: "ready"}}
		},
		func(_ context.Context, refs []string) map[string]model.ImageResult {
			if len(refs) != 1 || refs[0] != "bch-payments/manifests/v1" {
				t.Fatalf("image refs = %v", refs)
			}
			return map[string]model.ImageResult{
				refs[0]: {Reference: refs[0], URL: "https://nexus.example.test/repository/mlops/bch-payments/manifests/v1", Status: "exist", CheckedAt: &checkedAt},
			}
		},
	)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response model.DashboardResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.CronWorkflows) != 1 || response.CronWorkflows[0].Name != "daily-job" {
		t.Fatalf("dashboard workflows = %#v", response.CronWorkflows)
	}
	if len(response.CronWorkflows[0].Images) != 1 || response.CronWorkflows[0].Images[0].ImageID != "v1" || response.CronWorkflows[0].Images[0].URL != "https://nexus.example.test/repository/mlops/bch-payments/manifests/v1" || response.CronWorkflows[0].Images[0].Status != "exist" || response.RegistrySource.State != "ready" {
		t.Fatalf("image/source = %#v/%q", response.CronWorkflows[0].Images, response.RegistrySource.State)
	}
}
