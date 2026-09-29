package cluster

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"monitor/internal/model"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	cronWorkflowLabel = "workflows.argoproj.io/cron-workflow"
	workflowLabel     = "workflows.argoproj.io/workflow"
	scheduledTimeAnno = "workflows.argoproj.io/scheduled-time"
	emptyImageRef     = "(empty image reference)"
)

func BuildViews(snapshot Snapshot, now time.Time) []model.CronWorkflow {
	workflowsByCron := make(map[string][]*unstructured.Unstructured)
	for _, workflow := range snapshot.Workflows {
		cronName := workflow.GetLabels()[cronWorkflowLabel]
		if cronName != "" {
			key := resourceKey(workflow.GetNamespace(), cronName)
			workflowsByCron[key] = append(workflowsByCron[key], workflow)
		}
	}
	podsByWorkflow := make(map[string][]*unstructured.Unstructured)
	for _, pod := range snapshot.Pods {
		workflowName := pod.GetLabels()[workflowLabel]
		if workflowName != "" {
			key := resourceKey(pod.GetNamespace(), workflowName)
			podsByWorkflow[key] = append(podsByWorkflow[key], pod)
		}
	}

	views := make([]model.CronWorkflow, 0, len(snapshot.CronWorkflows))
	for _, cronWorkflow := range snapshot.CronWorkflows {
		view := model.CronWorkflow{
			Namespace: cronWorkflow.GetNamespace(),
			Name:      cronWorkflow.GetName(),
			Images:    make([]model.ImageResult, 0),
		}
		view.Suspended, _, _ = unstructured.NestedBool(cronWorkflow.Object, "spec", "suspend")
		active, _, _ := unstructured.NestedSlice(cronWorkflow.Object, "status", "active")
		view.Active = len(active) != 0
		view.LastScheduledAt = nestedTime(cronWorkflow.Object, "status", "lastScheduledTime")
		view.Schedules = cronSchedules(cronWorkflow.Object)
		view.Timezone, _, _ = unstructured.NestedString(cronWorkflow.Object, "spec", "timezone")

		workflowSpecImages := templateImages(cronWorkflow.Object, "spec", "workflowSpec", "templates")
		cronImages := append([]string(nil), workflowSpecImages...)
		latest := latestWorkflow(workflowsByCron[resourceKey(view.Namespace, view.Name)])
		if latest != nil {
			podObjects := podsByWorkflow[resourceKey(view.Namespace, latest.GetName())]
			run := workflowView(latest, podObjects)
			view.LastRun = &run
			cronImages = append(cronImages, templateImages(latest.Object, "spec", "templates")...)
			for _, pod := range run.Pods {
				cronImages = append(cronImages, pod.Images...)
			}
		}
		view.Images = imageResults(cronImages)
		imageIDs := make(map[string]string, len(workflowSpecImages))
		for _, image := range workflowSpecImages {
			imageIDs[image] = imageIDFromReference(image)
		}
		for i := range view.Images {
			view.Images[i].ImageID = imageIDs[view.Images[i].Reference]
		}
		if !view.Suspended {
			view.NextScheduledAt, view.ScheduleError = nextScheduledTime(view.Schedules, view.Timezone, now)
		}
		if view.Timezone == "" {
			view.Timezone = "UTC (assumed)"
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Namespace == views[j].Namespace {
			return views[i].Name < views[j].Name
		}
		return views[i].Namespace < views[j].Namespace
	})
	return views
}

func imageIDFromReference(image string) string {
	if image == emptyImageRef {
		return ""
	}
	parts := strings.Split(image, ":")
	return strings.TrimSpace(parts[len(parts)-1])
}

func workflowView(workflow *unstructured.Unstructured, pods []*unstructured.Unstructured) model.WorkflowRun {
	run := model.WorkflowRun{
		Name:        workflow.GetName(),
		CreatedAt:   creationTime(workflow),
		ScheduledAt: annotationTime(workflow, scheduledTimeAnno),
		Pods:        make([]model.Pod, 0, len(pods)),
	}
	run.Phase, _, _ = unstructured.NestedString(workflow.Object, "status", "phase")
	run.StartedAt = nestedTime(workflow.Object, "status", "startedAt")
	run.FinishedAt = nestedTime(workflow.Object, "status", "finishedAt")
	sort.Slice(pods, func(i, j int) bool { return pods[i].GetName() < pods[j].GetName() })
	for _, pod := range pods {
		view := model.Pod{Name: pod.GetName()}
		view.Phase, _, _ = unstructured.NestedString(pod.Object, "status", "phase")
		if view.Phase == "" {
			view.Phase = "Unknown"
		}
		view.ContainerStates = containerStates(pod.Object)
		view.Images = podImages(pod.Object)
		run.Pods = append(run.Pods, view)
	}
	return run
}

func latestWorkflow(workflows []*unstructured.Unstructured) *unstructured.Unstructured {
	var latest *unstructured.Unstructured
	var latestAt time.Time
	for _, workflow := range workflows {
		at := workflowTime(workflow)
		if latest == nil || at.After(latestAt) {
			latest = workflow
			latestAt = at
		}
	}
	return latest
}

func workflowTime(workflow *unstructured.Unstructured) time.Time {
	if scheduled := annotationTime(workflow, scheduledTimeAnno); scheduled != nil {
		return *scheduled
	}
	if created := creationTime(workflow); created != nil {
		return *created
	}
	return time.Time{}
}

func cronSchedules(object map[string]any) []string {
	schedules, found, _ := unstructured.NestedStringSlice(object, "spec", "schedules")
	if found && len(schedules) > 0 {
		return schedules
	}
	if schedule, found, _ := unstructured.NestedString(object, "spec", "schedule"); found && schedule != "" {
		return []string{schedule}
	}
	return nil
}

func nextScheduledTime(schedules []string, timezone string, now time.Time) (*time.Time, string) {
	if len(schedules) == 0 {
		return nil, "no schedule configured"
	}
	location := time.UTC
	if timezone != "" {
		var err error
		location, err = time.LoadLocation(timezone)
		if err != nil {
			return nil, "invalid timezone"
		}
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	var earliest time.Time
	for _, expression := range schedules {
		parsed, err := parser.Parse(expression)
		if err != nil {
			return nil, "invalid cron expression"
		}
		next := parsed.Next(now.In(location))
		if next.IsZero() {
			return nil, "next schedule unavailable"
		}
		if earliest.IsZero() || next.Before(earliest) {
			earliest = next
		}
	}
	if timezone == "" {
		return &earliest, "timezone unset; next run assumes UTC"
	}
	return &earliest, ""
}

func templateImages(object map[string]any, path ...string) []string {
	templates, _, _ := unstructured.NestedSlice(object, path...)
	images := make([]string, 0)
	for _, rawTemplate := range templates {
		template, ok := rawTemplate.(map[string]any)
		if !ok {
			continue
		}
		if container, found, _ := unstructured.NestedString(template, "container", "image"); found {
			images = appendImage(images, container)
		}
		if image, found, _ := unstructured.NestedString(template, "script", "image"); found {
			images = appendImage(images, image)
		}
		for _, field := range []string{"sidecars", "initContainers"} {
			items, _, _ := unstructured.NestedSlice(template, field)
			images = append(images, imagesFromItems(items)...)
		}
		containerSet, _, _ := unstructured.NestedSlice(template, "containerSet", "containers")
		images = append(images, imagesFromItems(containerSet)...)
	}
	return uniqueImages(images)
}

func podImages(object map[string]any) []string {
	var images []string
	for _, path := range [][]string{{"spec", "containers"}, {"spec", "initContainers"}, {"spec", "ephemeralContainers"}} {
		items, _, _ := unstructured.NestedSlice(object, path...)
		images = append(images, imagesFromItems(items)...)
	}
	return uniqueImages(images)
}

func imagesFromItems(items []any) []string {
	images := make([]string, 0, len(items))
	for _, item := range items {
		container, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if image, found, _ := unstructured.NestedString(container, "image"); found {
			images = appendImage(images, image)
		}
	}
	return images
}

func appendImage(images []string, image string) []string {
	image = strings.TrimSpace(image)
	if image == "" {
		image = emptyImageRef
	}
	return append(images, image)
}

func imageResults(images []string) []model.ImageResult {
	images = uniqueImages(images)
	results := make([]model.ImageResult, 0, len(images))
	for _, image := range images {
		results = append(results, model.ImageResult{Reference: image, Status: "unknown"})
	}
	return results
}

func uniqueImages(images []string) []string {
	seen := make(map[string]struct{}, len(images))
	unique := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, found := seen[image]; found {
			continue
		}
		seen[image] = struct{}{}
		unique = append(unique, image)
	}
	sort.Strings(unique)
	return unique
}

func containerStates(object map[string]any) []string {
	var states []string
	for _, field := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
		items, _, _ := unstructured.NestedSlice(object, "status", field)
		for _, item := range items {
			status, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(status, "name")
			state, found, _ := unstructured.NestedMap(status, "state")
			if !found {
				continue
			}
			for _, stateName := range []string{"waiting", "running", "terminated"} {
				detail, exists, _ := unstructured.NestedMap(state, stateName)
				if !exists {
					continue
				}
				reason, _, _ := unstructured.NestedString(detail, "reason")
				label := strings.ToUpper(stateName[:1]) + stateName[1:]
				if reason != "" {
					label += " (" + reason + ")"
				}
				states = append(states, fmt.Sprintf("%s: %s", name, label))
				break
			}
		}
	}
	sort.Strings(states)
	return states
}

func nestedTime(object map[string]any, path ...string) *time.Time {
	value, found, _ := unstructured.NestedString(object, path...)
	if !found || value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func annotationTime(object *unstructured.Unstructured, key string) *time.Time {
	value := object.GetAnnotations()[key]
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func creationTime(object *unstructured.Unstructured) *time.Time {
	created := object.GetCreationTimestamp().Time
	if created.IsZero() {
		return nil
	}
	created = created.UTC()
	return &created
}

func resourceKey(namespace, name string) string {
	return namespace + "/" + name
}
