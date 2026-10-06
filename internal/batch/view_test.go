package batch

import (
	"reflect"
	"testing"
	"time"

	"monitor/internal/testutil"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBuildViewsMapsLatestWorkflowPodsAndSchedule(t *testing.T) {
	now := time.Date(2026, 9, 27, 11, 15, 0, 0, time.UTC)

	views := BuildViews(
		testutil.LoadObjects(t, "testdata/cronworkflows.json"),
		testutil.LoadObjects(t, "testdata/workflows.json"),
		testutil.LoadObjects(t, "testdata/pods.json"),
		"bch-",
		now,
	)
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
	if len(view.History) != 2 || view.History[0].Name != "payout-latest" || view.History[1].Phase != "Failed" {
		t.Fatalf("history = %#v, want newest-first latest then failed older run", view.History)
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
		if image.ImageID != "v3" || image.Project != "payments" || image.Status != "unknown" {
			t.Fatalf("unresolved fixture image = %#v, want image ID v3 with unknown status", image)
		}
	}
	wantImages := []string{"registry.example.test/mlops/bch-payments/payout:v3"}
	if !reflect.DeepEqual(images, wantImages) {
		t.Fatalf("images = %v, want %v", images, wantImages)
	}
}

func TestProjectImagesFindsImagesWhereverTheyAre(t *testing.T) {
	spec := map[string]any{"workflowSpec": map[string]any{
		"arguments": map[string]any{"parameters": []any{
			map[string]any{"name": "image", "value": "repomaster.company.com/mlops/bch-yazi-girisi-model:ald7383"},
			map[string]any{"name": "note", "value": "runs bch-yazi-girisi-model nightly"},
		}},
		"templates": []any{
			map[string]any{"name": "run", "container": map[string]any{"image": "{{workflow.parameters.image}}"}},
			map[string]any{"name": "pinned", "script": map[string]any{"image": "repomaster.company.com/mlops/bch-kredi-skor@sha256:abc"}},
			map[string]any{"name": "tool", "container": map[string]any{"image": "busybox:1.36"}},
		},
	}}
	var got []string
	for _, image := range projectImages("bch-", spec) {
		got = append(got, image.Project+"@"+image.ImageID)
	}
	if want := []string{"kredi-skor@sha256:abc", "yazi-girisi-model@ald7383"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("project images = %v, want %v", got, want)
	}
}

func TestBuildViewsReadsImagesFromLastWorkflowForTemplateRefs(t *testing.T) {
	cron := testutil.LoadObjects(t, "testdata/cronworkflows.json")[0]
	cron.Object["spec"].(map[string]any)["workflowSpec"] = map[string]any{"workflowTemplateRef": map[string]any{"name": "payout"}}
	workflows := testutil.LoadObjects(t, "testdata/workflows.json")
	workflows[1].Object["status"].(map[string]any)["storedWorkflowTemplateSpec"] = map[string]any{"templates": []any{
		map[string]any{"name": "run", "container": map[string]any{"image": "registry.example.test/mlops/bch-payments:v9"}},
	}}
	views := BuildViews([]*unstructured.Unstructured{cron}, workflows, nil, "bch-", time.Now())
	if len(views[0].Images) != 1 || views[0].Images[0].ImageID != "v9" {
		t.Fatalf("images = %#v, want v9 from the last workflow", views[0].Images)
	}
}
