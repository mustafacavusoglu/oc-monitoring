package batch

import (
	"reflect"
	"testing"
	"time"

	"monitor/internal/testutil"
)

func TestBuildViewsMapsLatestWorkflowPodsAndSchedule(t *testing.T) {
	now := time.Date(2026, 9, 27, 11, 15, 0, 0, time.UTC)

	views := BuildViews(
		testutil.LoadObjects(t, "testdata/cronworkflows.json"),
		testutil.LoadObjects(t, "testdata/workflows.json"),
		testutil.LoadObjects(t, "testdata/pods.json"),
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
		if image.ImageID != "v3" || image.Status != "unknown" {
			t.Fatalf("unresolved fixture image = %#v, want image ID v3 with unknown status", image)
		}
	}
	wantImages := []string{"registry.example.test/payments-bch/payout:v3"}
	if !reflect.DeepEqual(images, wantImages) {
		t.Fatalf("images = %v, want %v", images, wantImages)
	}
}

func TestProjectImagesReadsEveryImageFieldWithTheNamespace(t *testing.T) {
	spec := map[string]any{"workflowSpec": map[string]any{
		"arguments": map[string]any{"parameters": []any{
			map[string]any{"name": "image", "value": "repomaster:5000/mlops/bch-yazi-girisi-model:not-an-image-field"},
		}},
		"templates": []any{
			map[string]any{"name": "run", "container": map[string]any{"image": "repomaster:5000/mlops/bch-yazi-girisi-model:ald7383"}},
			map[string]any{"name": "set", "containerSet": map[string]any{"containers": []any{
				map[string]any{"name": "a", "image": "repomaster/mlops/bch-yazi-girisi-model@sha256:abc"},
			}}},
			map[string]any{"name": "untagged", "script": map[string]any{"image": "repomaster:5000/mlops/bch-yazi-girisi-model"}},
			map[string]any{"name": "tool", "container": map[string]any{"image": "busybox:1.36"}},
		},
	}}
	var got []string
	for _, image := range projectImages(spec, "yazi-girisi-model") {
		got = append(got, image.ImageID)
	}
	// Parameter values are not image fields; an untagged image has no ID; busybox lacks the namespace.
	if want := []string{"sha256:abc", "ald7383"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("image IDs = %v, want %v", got, want)
	}
}
