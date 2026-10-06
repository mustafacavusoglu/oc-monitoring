// Package batch turns Argo CronWorkflows, their Workflows and pods into
// dashboard rows and resolves their project images against the registry.
package batch

import (
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

		view.Images = projectImages(cronWorkflow.Object["spec"], view.Namespace)
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

// projectImages returns every `image:` field of the CronWorkflow spec whose
// image name contains the namespace, wherever the field is (container, script,
// sidecar, init, containerSet or inline templates). Its ID is the part after
// the last ":" (or the digest after "@").
func projectImages(spec any, namespace string) []model.ImageResult {
	results := make([]model.ImageResult, 0)
	seen := make(map[string]bool)
	namespace = strings.ToLower(namespace)
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if image, ok := item.(string); ok && key == "image" {
					image = strings.TrimSpace(image)
					name, id := splitImage(image)
					if id != "" && !seen[image] && strings.Contains(strings.ToLower(name), namespace) {
						seen[image] = true
						results = append(results, model.ImageResult{Reference: image, ImageID: id, Status: model.ImageUnknown})
					}
					continue
				}
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(spec)
	sort.Slice(results, func(i, j int) bool { return results[i].Reference < results[j].Reference })
	return results
}

// splitImage splits "registry:5000/mlops/bch-proje:ald73" into its name and
// "ald73"; a colon before the last "/" is a registry port, not a tag.
func splitImage(image string) (name, id string) {
	if at := strings.LastIndex(image, "@"); at >= 0 {
		return image[:at], image[at+1:]
	}
	if colon := strings.LastIndex(image, ":"); colon > strings.LastIndex(image, "/") {
		return image[:colon], image[colon+1:]
	}
	return image, ""
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
		run.Pods = append(run.Pods, cluster.PodView(pod))
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
