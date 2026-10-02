// Package batch turns Argo CronWorkflows, their Workflows and pods into
// dashboard rows and resolves their project images against the registry.
package batch

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"monitor/internal/cluster"
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

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func BuildViews(cronWorkflows, workflows, pods []*unstructured.Unstructured, now time.Time) []model.CronWorkflow {
	workflowsByCron := groupByLabel(workflows, cronWorkflowLabel)
	podsByWorkflow := groupByLabel(pods, workflowLabel)

	views := make([]model.CronWorkflow, 0, len(cronWorkflows))
	for _, cronWorkflow := range cronWorkflows {
		view := model.CronWorkflow{
			Namespace:       cronWorkflow.GetNamespace(),
			Name:            cronWorkflow.GetName(),
			Schedules:       cronSchedules(cronWorkflow.Object),
			LastScheduledAt: cluster.NestedTime(cronWorkflow.Object, "status", "lastScheduledTime"),
		}
		view.Suspended, _, _ = unstructured.NestedBool(cronWorkflow.Object, "spec", "suspend")
		active, _, _ := unstructured.NestedFieldNoCopy(cronWorkflow.Object, "status", "active")
		activeRuns, _ := active.([]any)
		view.Active = len(activeRuns) != 0
		view.Timezone, _, _ = unstructured.NestedString(cronWorkflow.Object, "spec", "timezone")

		runs := workflowsByCron[cluster.Key(view.Namespace, view.Name)]
		sortNewestFirst(runs)
		view.History = make([]model.RunSummary, 0, len(runs))
		for _, run := range runs {
			view.History = append(view.History, runSummary(run))
		}
		if len(runs) > 0 {
			latest := workflowView(runs[0], podsByWorkflow[cluster.Key(view.Namespace, runs[0].GetName())])
			view.LastRun = &latest
		}

		view.Images = projectImages(cronWorkflow.Object, view.Namespace)
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

func groupByLabel(objects []*unstructured.Unstructured, label string) map[string][]*unstructured.Unstructured {
	groups := make(map[string][]*unstructured.Unstructured)
	for _, object := range objects {
		if owner := object.GetLabels()[label]; owner != "" {
			key := cluster.Key(object.GetNamespace(), owner)
			groups[key] = append(groups[key], object)
		}
	}
	return groups
}

// projectImages returns the workflowSpec images whose name contains the
// project namespace; only those are checked against the registry.
func projectImages(cronWorkflow map[string]any, namespace string) []model.ImageResult {
	results := make([]model.ImageResult, 0)
	for _, image := range templateImages(cronWorkflow, "spec", "workflowSpec", "templates") {
		if imageBelongsToProject(image, namespace) {
			results = append(results, model.ImageResult{Reference: image, ImageID: imageIDFromReference(image), Status: model.ImageUnknown})
		}
	}
	return results
}

func imageBelongsToProject(image, namespace string) bool {
	lastColon := strings.LastIndex(image, ":")
	if lastColon > strings.LastIndex(image, "/") {
		image = image[:lastColon]
	}
	return strings.Contains(strings.ToLower(image), strings.ToLower(namespace))
}

func imageIDFromReference(image string) string {
	if image == emptyImageRef {
		return ""
	}
	if at := strings.LastIndex(image, "@"); at > 0 {
		return image[:at]
	}
	lastColon := strings.LastIndex(image, ":")
	if lastColon < 0 || lastColon < strings.LastIndex(image, "/") {
		return ""
	}
	return strings.TrimSpace(image[lastColon+1:])
}

func runSummary(workflow *unstructured.Unstructured) model.RunSummary {
	phase, _, _ := unstructured.NestedString(workflow.Object, "status", "phase")
	if phase == "" {
		phase = "Pending"
	}
	return model.RunSummary{
		Name:       workflow.GetName(),
		Phase:      phase,
		StartedAt:  cluster.NestedTime(workflow.Object, "status", "startedAt"),
		FinishedAt: cluster.NestedTime(workflow.Object, "status", "finishedAt"),
	}
}

func workflowView(workflow *unstructured.Unstructured, pods []*unstructured.Unstructured) model.WorkflowRun {
	run := model.WorkflowRun{
		RunSummary:  runSummary(workflow),
		CreatedAt:   cluster.CreationTime(workflow),
		ScheduledAt: cluster.ParseTime(workflow.GetAnnotations()[scheduledTimeAnno]),
		Pods:        make([]model.Pod, 0, len(pods)),
	}
	sort.Slice(pods, func(i, j int) bool { return pods[i].GetName() < pods[j].GetName() })
	for _, pod := range pods {
		view := model.Pod{Name: pod.GetName(), ContainerStates: containerStates(pod.Object)}
		view.Phase, _, _ = unstructured.NestedString(pod.Object, "status", "phase")
		if view.Phase == "" {
			view.Phase = "Unknown"
		}
		run.Pods = append(run.Pods, view)
	}
	return run
}

func sortNewestFirst(workflows []*unstructured.Unstructured) {
	sort.SliceStable(workflows, func(i, j int) bool {
		return workflowTime(workflows[i]).After(workflowTime(workflows[j]))
	})
}

func workflowTime(workflow *unstructured.Unstructured) time.Time {
	if scheduled := cluster.ParseTime(workflow.GetAnnotations()[scheduledTimeAnno]); scheduled != nil {
		return *scheduled
	}
	if created := cluster.CreationTime(workflow); created != nil {
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
	var earliest time.Time
	for _, expression := range schedules {
		parsed, err := cronParser.Parse(expression)
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
	templates, _, _ := unstructured.NestedFieldNoCopy(object, path...)
	items, _ := templates.([]any)
	images := make([]string, 0)
	for _, rawTemplate := range items {
		template, ok := rawTemplate.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"container", "script"} {
			if image, found, _ := unstructured.NestedString(template, field, "image"); found {
				images = appendImage(images, image)
			}
		}
		for _, field := range [][]string{{"sidecars"}, {"initContainers"}, {"containerSet", "containers"}} {
			containers, _, _ := unstructured.NestedFieldNoCopy(template, field...)
			list, _ := containers.([]any)
			for _, item := range list {
				if container, ok := item.(map[string]any); ok {
					if image, found, _ := unstructured.NestedString(container, "image"); found {
						images = appendImage(images, image)
					}
				}
			}
		}
	}
	return uniqueSorted(images)
}

func appendImage(images []string, image string) []string {
	image = strings.TrimSpace(image)
	if image == "" {
		image = emptyImageRef
	}
	return append(images, image)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, found := seen[value]; !found {
			seen[value] = struct{}{}
			unique = append(unique, value)
		}
	}
	sort.Strings(unique)
	return unique
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
