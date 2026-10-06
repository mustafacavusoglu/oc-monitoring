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

// BuildViews builds a row per CronWorkflow. imagePrefix (e.g. "bch-") marks the
// image repository segment that names the Nexus repository to check.
func BuildViews(cronWorkflows, workflows, pods []*unstructured.Unstructured, imagePrefix string, now time.Time) []model.CronWorkflow {
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

		view.Images = projectImages(imagePrefix, cronWorkflow.Object["spec"])
		if len(view.Images) == 0 && len(runs) > 0 {
			// A workflowTemplateRef keeps the images out of the CronWorkflow; the
			// last Workflow stores the resolved template spec.
			view.Images = projectImages(imagePrefix, runs[0].Object["spec"], runs[0].Object["status"])
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

// projectImages finds every image reference with a "<prefix><project>"
// repository segment anywhere in the given objects: container, script,
// sidecar and init images, workflow parameter values (for templates that use
// "{{workflow.parameters.image}}") or a stored template spec.
// ponytail: matches any string that parses as such an image; tighten to known
// fields if a non-image string ever looks like one.
func projectImages(prefix string, objects ...any) []model.ImageResult {
	results := make([]model.ImageResult, 0)
	seen := make(map[string]bool)
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for _, item := range v {
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		case string:
			reference := strings.TrimSpace(v)
			if seen[reference] {
				return
			}
			if project, imageID := projectImage(reference, prefix); project != "" {
				seen[reference] = true
				results = append(results, model.ImageResult{Reference: reference, Project: project, ImageID: imageID, Status: model.ImageUnknown})
			}
		}
	}
	for _, object := range objects {
		walk(object)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Reference < results[j].Reference })
	return results
}

// projectImage returns the project named by the repository segment that
// starts with prefix, and the image tag or digest:
// registry.company.com/mlops/bch-yazi-girisi:ald73 → ("yazi-girisi", "ald73").
// A reference without a tag or digest, or with spaces or templating, is not
// an image to check.
func projectImage(reference, prefix string) (project, imageID string) {
	if reference == "" || strings.ContainsAny(reference, " \t\n{}") {
		return "", ""
	}
	ref := strings.ToLower(reference)
	if at := strings.LastIndex(ref, "@"); at >= 0 {
		ref, imageID = ref[:at], ref[at+1:]
	} else if colon := strings.LastIndex(ref, ":"); colon > strings.LastIndex(ref, "/") {
		ref, imageID = ref[:colon], ref[colon+1:]
	}
	if imageID == "" {
		return "", ""
	}
	segments := strings.Split(ref, "/")
	if len(segments) > 1 && strings.ContainsAny(segments[0], ".:") {
		segments = segments[1:] // registry host
	}
	for _, segment := range segments {
		if len(segment) > len(prefix) && strings.HasPrefix(segment, prefix) {
			return strings.TrimPrefix(segment, prefix), imageID
		}
	}
	return "", ""
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
