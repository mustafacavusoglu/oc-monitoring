package cluster

import (
	"fmt"
	"sort"
	"strings"

	"monitor/internal/model"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PodView summarises a pod: phase, ready containers, restarts and the state
// of every container.
func PodView(pod *unstructured.Unstructured) model.Pod {
	view := model.Pod{Name: pod.GetName(), StartedAt: NestedTime(pod.Object, "status", "startTime")}
	view.Phase, _, _ = unstructured.NestedString(pod.Object, "status", "phase")
	if view.Phase == "" {
		view.Phase = "Unknown"
	}
	view.Node, _, _ = unstructured.NestedString(pod.Object, "spec", "nodeName")
	raw, _, _ := unstructured.NestedFieldNoCopy(pod.Object, "status", "containerStatuses")
	statuses, _ := raw.([]any)
	view.Containers = len(statuses)
	for _, item := range statuses {
		if status, ok := item.(map[string]any); ok {
			if ready, _, _ := unstructured.NestedBool(status, "ready"); ready {
				view.Ready++
			}
			if restarts := NestedInt(status, "restartCount"); restarts != nil {
				view.Restarts += *restarts
			}
		}
	}
	view.ContainerStates = containerStates(pod.Object)
	return view
}

func containerStates(object map[string]any) []string {
	var states []string
	for _, field := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
		raw, _, _ := unstructured.NestedFieldNoCopy(object, "status", field)
		items, _ := raw.([]any)
		for _, item := range items {
			status, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(status, "name")
			for _, stateName := range []string{"waiting", "running", "terminated"} {
				detail, exists, _ := unstructured.NestedFieldNoCopy(status, "state", stateName)
				if !exists {
					continue
				}
				label := strings.ToUpper(stateName[:1]) + stateName[1:]
				if detailMap, ok := detail.(map[string]any); ok {
					if reason, _, _ := unstructured.NestedString(detailMap, "reason"); reason != "" {
						label += " (" + reason + ")"
					}
				}
				states = append(states, fmt.Sprintf("%s: %s", name, label))
				break
			}
		}
	}
	sort.Strings(states)
	return states
}
