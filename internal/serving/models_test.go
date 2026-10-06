package serving

import (
	"testing"

	"monitor/internal/config"
	"monitor/internal/model"
	"monitor/internal/testutil"
)

var rules = config.ServingRules{
	LLMImageKeywords:  []string{"vllm"},
	MLImageKeywords:   []string{"triton"},
	GPUResourceName:   "nvidia.com/gpu",
	MIGResourcePrefix: "nvidia.com/mig-",
}

func TestBuildModelsClassifiesByRuntimeImage(t *testing.T) {
	models := BuildModels(
		testutil.LoadObjects(t, "testdata/inferenceservices.json"),
		testutil.LoadObjects(t, "testdata/servingruntimes.json"),
		testutil.LoadObjects(t, "testdata/llminferenceservices.json"),
		nil,
		rules,
	)
	got := make(map[string]model.Model, len(models))
	for _, m := range models {
		got[m.Name] = m
	}
	if len(models) != 5 {
		t.Fatalf("models = %d (%v), want every InferenceService and LLMInferenceService", len(models), got)
	}

	llama := got["llama-chat"]
	if llama.Type != model.TypeLLM || llama.State != model.ModelReady || llama.GPU != 2 || *llama.MinReplicas != 1 || *llama.MaxReplicas != 3 {
		t.Fatalf("llama-chat = %+v", llama)
	}
	if llama.Image != "quay.io/modh/vllm:rhoai-2.19" || llama.URL == "" || llama.StorageURI != "s3://models/llama-3-8b" {
		t.Fatalf("llama-chat details = %+v", llama)
	}

	if len(llama.MIG) != 0 {
		t.Fatalf("llama-chat MIG = %v, want none", llama.MIG)
	}

	fraud := got["fraud-xgb"]
	if fraud.GPU != 0 || fraud.MIG["1g.5gb"] != 2 || fraud.MIG["3g.20gb"] != 1 {
		t.Fatalf("fraud-xgb accelerators = gpu %d mig %v, want 0 and 1g.5gb:2 3g.20gb:1", fraud.GPU, fraud.MIG)
	}
	if fraud.Type != model.TypeML || fraud.State != model.ModelNotReady || fraud.Reason != "RevisionMissing" || fraud.StateSince == nil {
		t.Fatalf("fraud-xgb = %+v", fraud)
	}

	granite := got["granite-rag"]
	if granite.Type != model.TypeLLM || granite.Kind != "LLMInferenceService" || granite.GPU != 1 || *granite.MinReplicas != 2 || granite.State != model.ModelNotReady {
		t.Fatalf("granite-rag = %+v", granite)
	}
}

func TestClassifyPrefersLLMKeywords(t *testing.T) {
	if got := classify([]string{"registry/vllm-triton-bridge:1"}, rules); got != model.TypeLLM {
		t.Fatalf("classify = %q, want llm", got)
	}
}

func TestBuildModelsTreatsOtherRuntimesAsCustomServeWithPods(t *testing.T) {
	models := BuildModels(
		testutil.LoadObjects(t, "testdata/inferenceservices.json"),
		testutil.LoadObjects(t, "testdata/servingruntimes.json"),
		nil,
		testutil.LoadObjects(t, "testdata/pods.json"),
		rules,
	)
	got := make(map[string]model.Model, len(models))
	for _, m := range models {
		got[m.Name] = m
	}
	// OpenVINO matches no keyword and the dangling runtime has no image.
	for _, name := range []string{"openvino-model", "dangling-runtime"} {
		if got[name].Type != model.TypeCustomServe {
			t.Fatalf("%s type = %q, want custom", name, got[name].Type)
		}
	}
	xgb := got["fraud-xgb"]
	if xgb.Type != model.TypeML || len(xgb.Pods) != 1 || xgb.Pods[0].Name != "fraud-xgb-predictor-abc" || xgb.Pods[0].Ready != 1 || xgb.Pods[0].Containers != 2 || xgb.Pods[0].Restarts != 7 {
		t.Fatalf("fraud-xgb = %+v, want an ML model with one pod 1/2 ready and 7 restarts", xgb)
	}
}
