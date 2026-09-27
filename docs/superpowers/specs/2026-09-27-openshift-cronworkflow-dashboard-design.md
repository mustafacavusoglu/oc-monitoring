# OpenShift Argo CronWorkflow Dashboard

## Goal

Build an on-premises dashboard that shows the health and recent executions of Argo Workflows `CronWorkflow` resources in OpenShift namespaces whose project metadata marks them as BCH serving projects.

The dashboard shows the last run and its Argo status, schedule and next scheduled run, related pod status, and whether each referenced container image tag exists in its registry. It must support filtering by namespace, workflow status, and image status.

## Confirmed inputs and assumptions

- The Azure Repos repository URL, branch, and project JSON path are deployment configuration.
- The project JSON is a top-level object keyed by OpenShift namespace. A project value contains fields such as `serving` and `team`:

  ```json
  {
    "example-namespace": {
      "serving": ["BCH"],
      "team": "example-team"
    }
  }
  ```

- A project is monitored when `serving` contains `BCH` (case-insensitive comparison). The object key is the namespace.
- The Go service runs inside OpenShift with a ServiceAccount.
- Registry image existence means a registry manifest lookup, not merely the current pod's pull status.
- Default project and image cache refresh is five minutes. The React page refreshes its API data every 30 seconds.
- The main table includes namespace, Argo status, and image status filters.

## Architecture

Use one Go service and a React single-page application served from the same container and origin. The browser calls the Go HTTP API; only the Go service accesses Azure Repos, the Kubernetes API, and image registries.

The Go service periodically fetches the configured Azure Repos file and parses the namespace map. It selects namespaces whose `serving` array contains `BCH`. Keep the last valid project map if a later fetch or parse fails, and report the source error and last successful refresh time.

Use Kubernetes shared informers for Argo `CronWorkflow`, Argo `Workflow`, and core `Pod` resources. Restrict informers to selected namespaces, and serve dashboard requests from their local cache rather than listing the cluster for each browser request. When the project namespace set changes, start informers for newly selected namespaces and stop informers for removed namespaces. The service performs read-only API calls.

Associate workflows with their parent CronWorkflow using the Argo `workflows.argoproj.io/cron-workflow` label. Associate pods with their workflow using the `workflows.argoproj.io/workflow` label. Use `CronWorkflow.status.lastScheduledTime` for the last schedule time. Select the latest retained child Workflow by its `workflows.argoproj.io/scheduled-time` annotation when present and creation time as a fallback. If Argo has pruned the child Workflow, report its run status as unavailable while retaining the CronWorkflow's last scheduled time.

Show the cron expression(s) and timezone and compute the earliest next scheduled time from them. Support both the legacy `spec.schedule` field and the `spec.schedules` list. Handle suspended CronWorkflows and invalid expressions explicitly.

For each distinct container image reference in the displayed CronWorkflow or latest Workflow, issue an OCI/Docker Registry v2 manifest `HEAD` request. Cache by normalized image reference for five minutes and bound concurrent registry requests. A successful manifest response means present, `404` means missing, and authentication, timeout, rate-limit, or other registry errors mean unknown. Do not collapse unknown into missing.

## Dashboard and API

The page has summary counts and a table. Rows show project namespace, CronWorkflow name, suspend/active state, latest Workflow run time and phase, pod names and phases, schedule/timezone, next run, and image references with status.

Provide filters for:

- Namespace: all or one monitored namespace.
- Workflow status: all or a specific Argo phase/status.
- Image status: all, present, missing, or unknown.

The page refreshes data every 30 seconds and displays source freshness and degraded-source indicators. Keep filtering client-side over the dashboard response. The API returns a consistent snapshot plus source health metadata. API errors should identify whether project metadata, Kubernetes access, or registry lookup is degraded.

## Configuration and deployment

Configure `AZURE_REPO_URL`, `AZURE_REPO_BRANCH`, and `AZURE_PROJECTS_PATH` with environment variables. Use the Azure DevOps Items REST API to fetch the selected file; mount its optional access token at `AZURE_TOKEN_FILE` as a read-only Secret file. Mount registry credentials as a Docker config JSON Secret file at `REGISTRY_AUTH_FILE`. Never send either credential to the browser or include either in logs. Configure project refresh interval and image cache TTL, defaulting both to five minutes.

Build React assets and the Go server into one container image. Deploy a read-only ServiceAccount, Deployment, and Service, with an OpenShift Route where needed. Grant only `get`, `list`, and `watch` on Argo `cronworkflows`, Argo `workflows`, and core `pods`, through a ClusterRole and ClusterRoleBinding. The binding permits reading these resources in all namespaces; it grants no write operations. If cluster-wide read access is not acceptable, use a RoleBinding in each BCH namespace instead.

The dashboard has no application-level sign-in in this version. Expose it only through the organization's existing internal network or ingress access controls.

## Failure behavior

- Azure fetch or JSON parse failure: keep the last valid namespace map, mark it stale, and show the error and last successful refresh time.
- Kubernetes informer/RBAC failure: show the affected source as degraded and do not present missing data as a healthy empty result.
- Registry authentication or network failure: show image status as unknown.
- Empty image reference, unsupported registry reference, or schedule expression: return an explicit unavailable/invalid state for that field without failing the full dashboard response.

## Acceptance criteria

1. Only project namespaces with `serving` containing `BCH` appear.
2. The dashboard reports each CronWorkflow's schedule, next scheduled time, last scheduled time, latest retained Workflow phase, and related pod phases.
3. Image status distinguishes present, missing, and unknown, and repeated checks use the cache.
4. Namespace, Workflow status, and image status filters work independently and together.
5. The browser never receives Azure or registry credentials and never accesses the Kubernetes API directly.
6. Dashboard reads use informer state; routine browser refreshes do not cause repeated cluster-wide list requests.
7. Source errors and stale data are visible and are not misrepresented as empty healthy results.
