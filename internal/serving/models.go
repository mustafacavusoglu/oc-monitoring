// Package serving classifies KServe deployments into LLM and ML models.
//
// An LLMInferenceService is always an LLM. An InferenceService is classified
// by the images of the ServingRuntime it names: a configured LLM keyword
// (e.g. vllm) makes it an LLM, a configured ML keyword (e.g. triton) an ML
// model; anything else is not shown. In a Custom Serve project namespace
// every model is a Custom Serve model, whatever its runtime, and carries its
// pods.
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

const inferenceServiceLabel = "serving.kserve.io/inferenceservice"

func BuildModels(inferenceServices, servingRuntimes, llmInferenceServices, pods []*unstructured.Unstructured, customServe map[string]bool, rules config.ServingRules) []model.Model {
	runtimes := make(map[string][]string, len(servingRuntimes))
	for _, runtime := range servingRuntimes {
		runtimes[cluster.Key(runtime.GetNamespace(), runtime.GetName())] = containerImages(runtime.Object, "spec", "containers")
	}
	podsByModel := make(map[string][]*unstructured.Unstructured)
	for _, pod := range pods {
		if owner := pod.GetLabels()[inferenceServiceLabel]; owner != "" {
			key := cluster.Key(pod.GetNamespace(), owner)
			podsByModel[key] = append(podsByModel[key], pod)
		}
	}

	models := make([]model.Model, 0, len(inferenceServices)+len(llmInferenceServices))
	for _, isvc := range inferenceServices {
		runtimeName, _, _ := unstructured.NestedString(isvc.Object, "spec", "predictor", "model", "runtime")
		images := runtimes[cluster.Key(isvc.GetNamespace(), runtimeName)]
		modelType := classify(images, rules)
		if customServe[isvc.GetNamespace()] {
			modelType = model.TypeCustomServe
		}
		if modelType == "" {
			continue
		}
		m := inferenceServiceModel(isvc, modelType, runtimeName, images, rules)
		if modelType == model.TypeCustomServe {
			m.Pods = podViews(podsByModel[cluster.Key(m.Namespace, m.Name)])
		}
		models = append(models, m)
	}
	for _, llmisvc := range llmInferenceServices {
		m := llmInferenceServiceModel(llmisvc, rules)
		if customServe[m.Namespace] {
			m.Type = model.TypeCustomServe
		}
		models = append(models, m)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Namespace == models[j].Namespace {
			return models[i].Name < models[j].Name
		}
		return models[i].Namespace < models[j].Namespace
	})
	return models
}

func podViews(pods []*unstructured.Unstructured) []model.Pod {
	views := make([]model.Pod, 0, len(pods))
	for _, pod := range pods {
		views = append(views, cluster.PodView(pod))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	return views
}

func inferenceServiceModel(isvc *unstructured.Unstructured, modelType, runtimeName string, images []string, rules config.ServingRules) model.Model {
	m := baseModel(isvc, modelType)
	m.Runtime = runtimeName
	m.Image = first(images)
	m.ModelFormat, _, _ = unstructured.NestedString(isvc.Object, "spec", "predictor", "model", "modelFormat", "name")
	m.StorageURI, _, _ = unstructured.NestedString(isvc.Object, "spec", "predictor", "model", "storageUri")
	m.MinReplicas = cluster.NestedInt(isvc.Object, "spec", "predictor", "minReplicas")
	m.MaxReplicas = cluster.NestedInt(isvc.Object, "spec", "predictor", "maxReplicas")
	addAccelerators(&m, isvc.Object, rules, "spec", "predictor", "model", "resources")
	return m
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
			addAccelerators(&m, container, rules, "resources")
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

// addAccelerators adds the full-GPU and MIG-slice counts of a resources block
// to m. MIG resources (e.g. nvidia.com/mig-1g.5gb) are keyed by profile.
func addAccelerators(m *model.Model, object map[string]any, rules config.ServingRules, resourcesPath ...string) {
	for name, count := range resourceCounts(object, resourcesPath) {
		switch {
		case name == rules.GPUResourceName:
			m.GPU += count
		case strings.HasPrefix(name, rules.MIGResourcePrefix):
			if m.MIG == nil {
				m.MIG = make(map[string]int64)
			}
			m.MIG[strings.TrimPrefix(name, rules.MIGResourcePrefix)] += count
		}
	}
}

// resourceCounts reads every resource quantity of a resources block; a limit
// overrides the request of the same resource.
func resourceCounts(object map[string]any, resourcesPath []string) map[string]int64 {
	counts := make(map[string]int64)
	for _, kind := range []string{"requests", "limits"} {
		raw, _, _ := unstructured.NestedFieldNoCopy(object, append(append([]string{}, resourcesPath...), kind)...)
		quantities, _ := raw.(map[string]any)
		for name, value := range quantities {
			if quantity, err := resource.ParseQuantity(fmt.Sprint(value)); err == nil {
				counts[name] = quantity.Value()
			}
		}
	}
	return counts
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
