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
		nil, nil,
		rules,
	)
	got := make(map[string]model.Model, len(models))
	for _, m := range models {
		got[m.Name] = m
	}
	if len(models) != 3 {
		t.Fatalf("models = %d (%v), want llama-chat, fraud-xgb, granite-rag; unmatched runtimes are excluded", len(models), got)
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
	if got := classify(nil, rules); got != "" {
		t.Fatalf("classify(nil) = %q, want empty", got)
	}
}

func TestBuildModelsTreatsCustomServeNamespacesAsCustomWithPods(t *testing.T) {
	pods := testutil.LoadObjects(t, "testdata/pods.json")
	models := BuildModels(
		testutil.LoadObjects(t, "testdata/inferenceservices.json"),
		testutil.LoadObjects(t, "testdata/servingruntimes.json"),
		nil,
		pods,
		map[string]bool{"fraud": true},
		rules,
	)
	got := make(map[string]model.Model, len(models))
	for _, m := range models {
		got[m.Name] = m
	}
	// openvino-model matches no runtime keyword; Custom Serve still shows it.
	for _, name := range []string{"fraud-xgb", "openvino-model"} {
		if got[name].Type != model.TypeCustomServe {
			t.Fatalf("%s type = %q, want custom", name, got[name].Type)
		}
	}
	xgb := got["fraud-xgb"].Pods
	if len(xgb) != 1 || xgb[0].Name != "fraud-xgb-predictor-abc" || xgb[0].Ready != 1 || xgb[0].Containers != 2 || xgb[0].Restarts != 7 {
		t.Fatalf("fraud-xgb pods = %+v, want one pod 1/2 ready with 7 restarts", xgb)
	}
	if got["llama-chat"].Type != model.TypeLLM || got["llama-chat"].Pods != nil {
		t.Fatalf("llama-chat outside Custom Serve = %+v, want LLM without pods", got["llama-chat"])
	}
}
