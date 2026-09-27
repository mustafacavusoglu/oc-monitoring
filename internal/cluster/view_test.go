package cluster

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBuildViewsMapsLatestWorkflowPodsAndSchedule(t *testing.T) {
	snapshot := Snapshot{
		CronWorkflows: loadObjects(t, "testdata/cronworkflows.json"),
		Workflows:     loadObjects(t, "testdata/workflows.json"),
		Pods:          loadObjects(t, "testdata/pods.json"),
	}
	now := time.Date(2026, 9, 27, 11, 15, 0, 0, time.UTC)

	views := BuildViews(snapshot, now)
	if len(views) != 1 {
		t.Fatalf("workflow rows = %d, want 1", len(views))
	}
	view := views[0]
	if view.Namespace != "payments-bch" || view.Name != "payout-batch" || !view.Active {
		t.Fatalf("cron workflow identity/state = %s/%s active=%v", view.Namespace, view.Name, view.Active)
	}
	if view.LastRun == nil || view.LastRun.Name != "payout-latest" || view.LastRun.Phase != "Running" {
		t.Fatalf("last run = %#v, want payout-latest Running", view.LastRun)
	}
	if view.LastRun.ScheduledAt == nil || !view.LastRun.ScheduledAt.Equal(time.Date(2026, 9, 27, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("scheduled time = %v", view.LastRun.ScheduledAt)
	}
	if len(view.LastRun.Pods) != 1 || view.LastRun.Pods[0].Name != "payout-step" || view.LastRun.Pods[0].Phase != "Running" {
		t.Fatalf("related pods = %#v, want only running payout-step", view.LastRun.Pods)
	}
	if view.NextScheduledAt == nil || !view.NextScheduledAt.Equal(time.Date(2026, 9, 27, 11, 30, 0, 0, time.UTC)) {
		t.Fatalf("next run = %v, want 2026-09-27T11:30:00Z", view.NextScheduledAt)
	}
	images := make([]string, 0, len(view.Images))
	for _, image := range view.Images {
		images = append(images, image.Reference)
		if image.Status != "unknown" {
			t.Fatalf("unresolved fixture image %q status = %q, want unknown", image.Reference, image.Status)
		}
	}
	wantImages := []string{"(empty image reference)", "registry.example.test/payments/payout:v3"}
	if !reflect.DeepEqual(images, wantImages) {
		t.Fatalf("images = %v, want %v", images, wantImages)
	}
}

func loadObjects(t *testing.T, file string) []*unstructured.Unstructured {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var objects []map[string]any
	if err := json.Unmarshal(data, &objects); err != nil {
		t.Fatal(err)
	}
	result := make([]*unstructured.Unstructured, 0, len(objects))
	for _, object := range objects {
		result = append(result, &unstructured.Unstructured{Object: object})
	}
	return result
}
