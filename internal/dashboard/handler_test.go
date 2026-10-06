package dashboard

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"monitor/internal/cluster"
	"monitor/internal/config"
	"monitor/internal/model"
	"monitor/internal/projects"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var resources = config.Resources{
	CronWorkflows:        schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "cronworkflows"},
	Workflows:            schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "workflows"},
	InferenceServices:    schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1beta1", Resource: "inferenceservices"},
	ServingRuntimes:      schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1alpha1", Resource: "servingruntimes"},
	LLMInferenceServices: schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1alpha1", Resource: "llminferenceservices"},
}

type fakeImages struct{ t *testing.T }

func (fakeImages) ManifestURL(namespace, imageID string) string {
	return "https://nexus.example.test/bch-" + namespace + "/manifests/" + imageID
}

func (f fakeImages) Lookup(_ context.Context, urls []string) map[string]model.ImageResult {
	want := "https://nexus.example.test/bch-payments/manifests/v1"
	if len(urls) != 1 || urls[0] != want {
		f.t.Fatalf("image URLs = %v, want [%s]", urls, want)
	}
	checkedAt := time.Date(2026, 9, 27, 12, 1, 0, 0, time.UTC)
	return map[string]model.ImageResult{want: {URL: want, Status: model.ImageExist, CheckedAt: &checkedAt}}
}

func object(apiVersion, kind, namespace, name string, spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion, "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": namespace},
		"spec":     spec,
	}}
}

func namespace(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": name}}}
}

func TestDashboardEndpointCombinesBatchAndModels(t *testing.T) {
	cron := object("argoproj.io/v1alpha1", "CronWorkflow", "payments", "daily-job", map[string]any{
		"schedule": "0 12 * * *",
		"workflowSpec": map[string]any{"templates": []any{
			map[string]any{"name": "run", "container": map[string]any{"image": "registry.example.test/mlops/bch-payments:v1"}},
		}},
	})
	withImage := func(namespace, image string) *unstructured.Unstructured {
		copy := cron.DeepCopy()
		copy.SetNamespace(namespace)
		copy.Object["spec"].(map[string]any)["workflowSpec"] = map[string]any{"templates": []any{
			map[string]any{"name": "run", "container": map[string]any{"image": image}},
		}}
		return copy
	}
	otherTeam := withImage("other-team", "busybox:1.36")
	cmCron := withImage("cm-proje", "registry.example.test/cm/app:v2")
	runtime := object("serving.kserve.io/v1alpha1", "ServingRuntime", "chatbot", "vllm", map[string]any{
		"containers": []any{map[string]any{"image": "quay.io/vllm/vllm-openai:0.6"}},
	})
	isvc := object("serving.kserve.io/v1beta1", "InferenceService", "chatbot", "llama", map[string]any{
		"predictor": map[string]any{"model": map[string]any{"runtime": "vllm"}},
	})
	api := NewHandler(Sources{
		Projects: func() projects.Snapshot {
			return projects.Snapshot{Projects: []model.Project{
				{Key: "PAYMENTS", Namespace: "payments"},
				{Key: "GHOST", Namespace: "ghost"},
				{Key: "CM_PROJE", Namespace: "cm-proje"},
			}}
		},
		Cluster: func() cluster.Snapshot {
			return cluster.Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{
				resources.CronWorkflows:   {cron, otherTeam, cmCron},
				resources.ServingRuntimes: {runtime}, resources.InferenceServices: {isvc},
				cluster.Namespaces: {namespace("payments"), namespace("chatbot"), namespace("other-team"), namespace("cm-proje")},
			}, Health: model.SourceHealth{State: model.StateReady}}
		},
		Pods:   func() cluster.Snapshot { return cluster.Snapshot{Health: model.SourceHealth{State: model.StateReady}} },
		Images: fakeImages{t},
	}, config.Config{
		Resources:         resources,
		Serving:           config.ServingRules{LLMImageKeywords: []string{"vllm"}, MLImageKeywords: []string{"triton"}},
		UIRefreshInterval: 30 * time.Second,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("HTTP %d encoding=%q: %s", recorder.Code, recorder.Header().Get("Content-Encoding"), recorder.Body.String())
	}
	body, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	var response model.DashboardResponse
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		t.Fatal(err)
	}

	if response.RefreshIntervalSeconds != 30 {
		t.Fatalf("refresh interval = %d, want 30", response.RefreshIntervalSeconds)
	}
	// Every CronWorkflow in the cluster is shown; images with a bch- segment are checked.
	var rows []string
	for _, row := range response.CronWorkflows {
		rows = append(rows, fmt.Sprintf("%s:%d", row.Namespace, len(row.Images)))
	}
	if want := []string{"cm-proje:0", "other-team:0", "payments:1"}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("cron workflows (namespace:images) = %v, want %v", rows, want)
	}
	if response.CronWorkflows[2].Images[0].Status != model.ImageExist || response.Sources.Registry.State != model.StateReady {
		t.Fatalf("payments images = %#v, registry = %#v", response.CronWorkflows[2].Images, response.Sources.Registry)
	}
	if len(response.Models) != 1 || response.Models[0].Type != model.TypeLLM || response.Models[0].Name != "llama" {
		t.Fatalf("models = %#v", response.Models)
	}
	if want := []string{"chatbot", "cm-proje", "ghost", "other-team", "payments"}; !reflect.DeepEqual(response.Namespaces, want) {
		t.Fatalf("namespaces = %v, want %v", response.Namespaces, want)
	}
	// Coverage shows the project without a namespace instead of hiding it.
	want := []model.Project{
		{Key: "PAYMENTS", Namespace: "payments", NamespaceExists: true, CronWorkflows: 1},
		{Key: "GHOST", Namespace: "ghost"},
		{Key: "CM_PROJE", Namespace: "cm-proje", NamespaceExists: true, CronWorkflows: 1},
	}
	if !reflect.DeepEqual(response.Projects, want) {
		t.Fatalf("projects = %+v, want %+v", response.Projects, want)
	}
}
