package serving

import (
	"testing"

	"monitor/internal/config"
	"monitor/internal/model"
	"monitor/internal/testutil"
)

var rules = config.ServingRules{
	LLMImageKeywords: []string{"vllm"},
	MLImageKeywords:  []string{"triton"},
	GPUResourceName:  "nvidia.com/gpu",
}

func TestBuildModelsClassifiesByRuntimeImage(t *testing.T) {
	models := BuildModels(
		testutil.LoadObjects(t, "testdata/inferenceservices.json"),
		testutil.LoadObjects(t, "testdata/servingruntimes.json"),
		testutil.LoadObjects(t, "testdata/llminferenceservices.json"),
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

	fraud := got["fraud-xgb"]
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
