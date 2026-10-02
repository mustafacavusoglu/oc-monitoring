// Package serving classifies KServe deployments into LLM and ML models.
//
// An LLMInferenceService is always an LLM. An InferenceService is classified
// by the images of the ServingRuntime it names: a configured LLM keyword
// (e.g. vllm) makes it an LLM, a configured ML keyword (e.g. triton) an ML
// model; anything else is not shown.
package serving

import (
	"fmt"
	"sort"
	"strings"

	"monitor/internal/cluster"
	"monitor/internal/config"
	"monitor/internal/model"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func BuildModels(inferenceServices, servingRuntimes, llmInferenceServices []*unstructured.Unstructured, rules config.ServingRules) []model.Model {
	runtimes := make(map[string][]string, len(servingRuntimes))
	for _, runtime := range servingRuntimes {
		runtimes[cluster.Key(runtime.GetNamespace(), runtime.GetName())] = containerImages(runtime.Object, "spec", "containers")
	}

	models := make([]model.Model, 0, len(inferenceServices)+len(llmInferenceServices))
	for _, isvc := range inferenceServices {
		if m, ok := inferenceServiceModel(isvc, runtimes, rules); ok {
			models = append(models, m)
		}
	}
	for _, llmisvc := range llmInferenceServices {
		models = append(models, llmInferenceServiceModel(llmisvc, rules))
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Namespace == models[j].Namespace {
			return models[i].Name < models[j].Name
		}
		return models[i].Namespace < models[j].Namespace
	})
	return models
}

func inferenceServiceModel(isvc *unstructured.Unstructured, runtimes map[string][]string, rules config.ServingRules) (model.Model, bool) {
	runtimeName, _, _ := unstructured.NestedString(isvc.Object, "spec", "predictor", "model", "runtime")
	images := runtimes[cluster.Key(isvc.GetNamespace(), runtimeName)]
	modelType := classify(images, rules)
	if modelType == "" {
		return model.Model{}, false
	}
	m := baseModel(isvc, modelType)
	m.Runtime = runtimeName
	m.Image = first(images)
	m.ModelFormat, _, _ = unstructured.NestedString(isvc.Object, "spec", "predictor", "model", "modelFormat", "name")
	m.StorageURI, _, _ = unstructured.NestedString(isvc.Object, "spec", "predictor", "model", "storageUri")
	m.MinReplicas = cluster.NestedInt(isvc.Object, "spec", "predictor", "minReplicas")
	m.MaxReplicas = cluster.NestedInt(isvc.Object, "spec", "predictor", "maxReplicas")
	m.GPU = gpuCount(isvc.Object, rules.GPUResourceName, "spec", "predictor", "model", "resources")
	return m, true
}

func llmInferenceServiceModel(llmisvc *unstructured.Unstructured, rules config.ServingRules) model.Model {
	m := baseModel(llmisvc, model.TypeLLM)
	m.Image = first(containerImages(llmisvc.Object, "spec", "template", "containers"))
	m.ModelFormat, _, _ = unstructured.NestedString(llmisvc.Object, "spec", "model", "name")
	m.StorageURI, _, _ = unstructured.NestedString(llmisvc.Object, "spec", "model", "uri")
	m.MinReplicas = cluster.NestedInt(llmisvc.Object, "spec", "replicas")
	m.MaxReplicas = m.MinReplicas
	containers, _, _ := unstructured.NestedFieldNoCopy(llmisvc.Object, "spec", "template", "containers")
	items, _ := containers.([]any)
	for _, item := range items {
		if container, ok := item.(map[string]any); ok {
			m.GPU += gpuCount(container, rules.GPUResourceName, "resources")
		}
	}
	return m
}

func baseModel(object *unstructured.Unstructured, modelType string) model.Model {
	m := model.Model{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
		Kind:      object.GetKind(),
		Type:      modelType,
		CreatedAt: cluster.CreationTime(object),
		State:     model.ModelUnknown,
	}
	m.URL, _, _ = unstructured.NestedString(object.Object, "status", "url")
	conditions, _, _ := unstructured.NestedFieldNoCopy(object.Object, "status", "conditions")
	items, _ := conditions.([]any)
	for _, item := range items {
		condition, ok := item.(map[string]any)
		if !ok || condition["type"] != "Ready" {
			continue
		}
		switch condition["status"] {
		case "True":
			m.State = model.ModelReady
		case "False":
			m.State = model.ModelNotReady
		}
		m.Reason, _, _ = unstructured.NestedString(condition, "reason")
		m.Message, _, _ = unstructured.NestedString(condition, "message")
		m.StateSince = cluster.NestedTime(condition, "lastTransitionTime")
	}
	return m
}

// classify checks LLM keywords first so a runtime image matching both is an LLM.
func classify(images []string, rules config.ServingRules) string {
	switch {
	case matchesAny(images, rules.LLMImageKeywords):
		return model.TypeLLM
	case matchesAny(images, rules.MLImageKeywords):
		return model.TypeML
	default:
		return ""
	}
}

func matchesAny(images, keywords []string) bool {
	for _, image := range images {
		image = strings.ToLower(image)
		for _, keyword := range keywords {
			if strings.Contains(image, strings.ToLower(keyword)) {
				return true
			}
		}
	}
	return false
}

func containerImages(object map[string]any, path ...string) []string {
	containers, _, _ := unstructured.NestedFieldNoCopy(object, path...)
	items, _ := containers.([]any)
	images := make([]string, 0, len(items))
	for _, item := range items {
		if container, ok := item.(map[string]any); ok {
			if image, _, _ := unstructured.NestedString(container, "image"); image != "" {
				images = append(images, image)
			}
		}
	}
	return images
}

// gpuCount reads the GPU limit (falling back to the request) of a resources block.
func gpuCount(object map[string]any, gpuResource string, resourcesPath ...string) int64 {
	for _, kind := range []string{"limits", "requests"} {
		path := append(append([]string{}, resourcesPath...), kind, gpuResource)
		if raw, found, _ := unstructured.NestedFieldNoCopy(object, path...); found {
			if quantity, err := resource.ParseQuantity(fmt.Sprint(raw)); err == nil {
				return quantity.Value()
			}
		}
	}
	return 0
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
