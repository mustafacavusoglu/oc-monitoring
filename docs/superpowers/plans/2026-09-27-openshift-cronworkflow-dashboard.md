# OpenShift Argo CronWorkflow Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a read-only React dashboard and Go API for BCH project CronWorkflows, their latest Workflow and Pods, schedules, and registry image status.

**Architecture:** One Go process serves the SPA and API. It refreshes project metadata from Azure Repos, maintains selected-namespace Kubernetes informer caches, checks distinct images against registries with a bounded TTL cache, and returns one dashboard snapshot to the browser.

**Tech Stack:** Go, Kubernetes `client-go` dynamic informers, `go-containerregistry` for registry auth and manifest checks, React, TypeScript, Vite, Docker, OpenShift YAML.

**Spec:** `docs/superpowers/specs/2026-09-27-openshift-cronworkflow-dashboard-design.md`

## Global Constraints

- Monitor top-level JSON object keys whose project `serving` array contains `BCH` (case-insensitive); each key is an OpenShift namespace.
- The Go service runs in OpenShift with a ServiceAccount and reads Argo `cronworkflows`, Argo `workflows`, and core `pods` only.
- Use shared informer state for dashboard reads; browser refreshes do not cause Kubernetes list calls.
- Keep Azure and registry credentials in mounted Secret files; never return or log them.
- Default Azure project refresh and image cache TTL to five minutes; refresh the browser every 30 seconds.
- Return `present`, `missing`, or `unknown` image status; only registry `404` means missing.
- Required filters: namespace, Workflow status, and image status.
- Preserve last valid project data on source errors and show source health/freshness.
- No application-level sign-in; deployment relies on existing internal network or ingress controls.
- Do not add or run tests unless requested; use compile/build checks and review the listed failure cases.

## Review Focus

- Malformed or unavailable Azure JSON must preserve the last valid namespace snapshot and report stale source health (Task 2).
- A removed namespace must stop being watched, while a newly selected namespace starts appearing after informer sync (Task 3).
- Argo history pruning or absent scheduled-time annotation must not invent a Workflow run time/status (Task 3).
- Registry auth, rate limit, timeout, or malformed image reference must remain `unknown`, never `missing` (Task 4).
- A CronWorkflow with multiple images must match the image filter when any image has the selected status (Task 5).

## File Map

- `go.mod`, `cmd/dashboard/main.go`: Go module and process wiring.
- `internal/config/config.go`: environment and mounted-file configuration.
- `internal/projects/azure.go`, `internal/projects/projects.go`: Azure Repos refresh and BCH namespace snapshot.
- `internal/model/types.go`: shared dashboard domain and API response types.
- `internal/cluster/watch.go`, `internal/cluster/view.go`: namespace informer lifecycle and Argo/Pod view mapping.
- `internal/registry/images.go`: image-reference normalization, registry HEAD checks, and TTL/concurrency cache.
- `internal/dashboard/handler.go`: API response and snapshot aggregation.
- `internal/webui/server.go`: SPA file serving and API mux wiring.
- `web/package.json`, `web/package-lock.json`, `web/index.html`, `web/src/*`, `web/vite.config.ts`: React SPA.
- `Dockerfile`, `.dockerignore`: single-image build.
- `deploy/openshift.yaml`: ServiceAccount, ClusterRole/Binding, Deployment, Service, Route.
- `README.md`: configuration, secret mounts, RBAC, and deployment steps.

### Task 1: Scaffold the Go service, SPA, and single-image build

**Files:** Create the module/process files, `internal/config/config.go`, `internal/model/types.go`, `internal/webui/server.go`, React/Vite entry files, `Dockerfile`, and `.dockerignore`.

**Interfaces:** `config.Load() (config.Config, error)` reads `HTTP_ADDR` (default `:8080`), `AZURE_REPO_URL`, `AZURE_REPO_BRANCH`, `AZURE_PROJECTS_PATH`, optional `AZURE_TOKEN_FILE`, optional `REGISTRY_AUTH_FILE`, `PROJECT_REFRESH_INTERVAL`, `IMAGE_CACHE_TTL`, and `REGISTRY_CHECK_CONCURRENCY`. `webui.New(api http.Handler, webDir string) http.Handler` serves `/api/*` through the supplied handler and SPA assets for other paths.

- [x] Create the Go module with `client-go`, `go-containerregistry`, and the cron parser dependencies; keep Kubernetes libraries on one compatible minor version.
- [x] Implement config parsing, duration defaults (5 minutes), required Azure source validation, and mounted secret-file reads without logging secret contents. For the registry Secret, require a Docker config named `config.json`, use its mounted path as `REGISTRY_AUTH_FILE`, and point `DOCKER_CONFIG` at the containing directory.
- [x] Define shared models: `ImageResult{Reference, Status string; CheckedAt *time.Time; Error string}`, `Pod{Name, Phase string; ContainerStates, Images []string}`, `WorkflowRun{Name, Phase string; ScheduledAt, CreatedAt, StartedAt, FinishedAt *time.Time; Pods []Pod}`, `CronWorkflow{Namespace, Name string; Schedules []string; Timezone string; Suspended, Active bool; LastScheduledAt, NextScheduledAt *time.Time; LastRun *WorkflowRun; Images []ImageResult}`, `SourceHealth{State string; LastSuccess *time.Time; Error string}`, and `DashboardResponse{GeneratedAt time.Time; ProjectSource, ClusterSource, RegistrySource SourceHealth; Namespaces []string; CronWorkflows []CronWorkflow}`. These keep cluster, registry, API, and frontend data mapping acyclic.
- [x] Create the Vite React TypeScript scaffold with a minimal page shell and `npm run build` script.
- [x] Wire `cmd/dashboard/main.go` to load config, serve `/healthz`, serve static assets, and shut down the HTTP server on SIGTERM.
- [x] Add a multi-stage Dockerfile that builds the SPA, compiles the Go binary, and copies both into a non-root runtime image.
- [x] Check `go build ./...` and `npm run build`.

### Task 2: Load and cache BCH project namespaces from Azure Repos

**Files:** Create `internal/projects/azure.go` and `internal/projects/projects.go`.

**Interfaces:** `projects.NewSource(cfg config.Config, client *http.Client) *projects.Source`; `(*Source).Run(ctx context.Context)` refreshes in the background; `(*Source).Snapshot() projects.Snapshot` returns sorted namespaces, update time, stale state, and last error. The Azure client builds the Items REST request from repository URL, branch, and file path and authenticates from `AZURE_TOKEN_FILE` when configured.

- [x] Implement Azure DevOps Items REST URL construction (`api-version=7.1`), branch/path query encoding, optional PAT authentication, request timeout, and response status handling.
- [x] Parse the top-level namespace map; select entries with a `serving` string array containing `BCH`, case-insensitively. A malformed top-level map or non-array `serving` value fails the refresh; an absent `serving` field does not select that project.
- [x] Refresh at the configured interval; replace the snapshot only after a successful fetch and parse; otherwise keep last good data and publish stale/error metadata.
- [x] Check `go build ./...`.

### Task 3: Watch selected Argo and Pod resources

**Files:** Create `internal/cluster/watch.go` and `internal/cluster/view.go`.

**Interfaces:** `cluster.NewWatcher(cfg *rest.Config, namespaces func() []string) (*Watcher, error)`; `(*Watcher).Run(ctx context.Context)` reconciles namespace informers; `(*Watcher).Snapshot() cluster.Snapshot` returns cached CronWorkflow/Workflow/Pod objects and source health. `cluster.BuildViews(snapshot Snapshot, now time.Time) []model.CronWorkflow` maps unstructured resources into shared dashboard domain types.

- [x] Create per-namespace dynamic shared informers for `argoproj.io/v1alpha1` CronWorkflows and Workflows plus core Pods; start watches only for the selected namespaces.
- [x] Reconcile namespace changes by adding new informer sets and stopping removed ones; wait for cache sync before marking a new namespace healthy.
- [x] Map workflows by `workflows.argoproj.io/cron-workflow`; map pods by `workflows.argoproj.io/workflow`.
- [x] Use `status.lastScheduledTime`, workflow `workflows.argoproj.io/scheduled-time` annotation (fallback: creation time), Workflow phase, Pod phase/container state, and CronWorkflow suspend/schedule/timezone.
- [x] Support `spec.schedule` and `spec.schedules`; compute the earliest next run with the schedule timezone; suspended or invalid schedules have explicit state.
- [x] Extract distinct inline image references from CronWorkflow/Workflow templates and actual related Pod containers; missing image data remains unavailable.
- [x] Check `go build ./...`.

### Task 4: Check image existence with registry auth and caching

**Files:** Create `internal/registry/images.go`.

**Interfaces:** `registry.NewChecker(authFile string, ttl time.Duration, maxConcurrent int) (*Checker, error)`; `(*Checker).Check(ctx context.Context, refs []string) map[string]model.ImageResult`. `model.ImageResult` contains reference, status (`present|missing|unknown`), checked time, and a safe error summary.

- [x] Normalize image references and use `authn.DefaultKeychain` to load the mounted Docker config JSON from `DOCKER_CONFIG`.
- [x] Use `remote.Head` with request context/timeout; map success to present, registry 404 to missing, and auth/network/rate-limit/unsupported reference errors to unknown.
- [x] Cache results by normalized image reference for the configured TTL and bound concurrent registry requests.
- [x] Check `go build ./...`.

### Task 5: Aggregate and serve the dashboard API

**Files:** Create `internal/dashboard/handler.go`; update `cmd/dashboard/main.go` and `internal/webui/server.go`.

**Interfaces:** `dashboard.NewHandler(projects *projects.Source, watcher *cluster.Watcher, checker *registry.Checker) http.Handler`. `model.DashboardResponse` contains `generatedAt`, source health/freshness, namespaces, and CronWorkflow rows. Each row includes schedule(s), timezone, next/last schedule time, latest run/phase, pods, and image results.

- [x] Build a response from the latest project and informer snapshots, issue image checks only for distinct cache keys, and return degraded metadata instead of presenting source failure as an empty healthy list.
- [x] Define latest Workflow selection by scheduled-time annotation, falling back to creation timestamp; if history is absent, preserve CronWorkflow lastScheduledTime and mark run status unavailable.
- [x] Handle partial registry/schedule/image-data errors per field so one item does not fail the full response.
- [x] Check `go build ./...`.
- [x] Inspect a representative JSON response from a local handler setup using fixed project/cluster snapshots and a controlled registry result.

### Task 6: Build the React dashboard and required filters

**Files:** Create `web/src/api.ts`, `web/src/types.ts`, `web/src/App.tsx`, `web/src/styles.css`; update `web/src/main.tsx`.

**Interfaces:** `fetchDashboard(signal?: AbortSignal): Promise<DashboardResponse>`; React state holds namespace, Workflow status, and image status filter values. A row matches the image filter if any of its images has the selected status.

- [x] Fetch `/api/dashboard` on load and every 30 seconds; cancel stale requests and show last update/source degradation.
- [x] Render summary counts and rows for namespace, CronWorkflow, schedule/next time, last run/status, pod phase, and image references/status.
- [x] Add independent namespace, Workflow status, and image status filters; include `all` choices and combine selected filters.
- [x] Render explicit loading, no-result, stale-source, and partial/unknown states; keep the layout usable on narrow screens.
- [x] Check `npm run build`.

### Task 7: Add OpenShift deployment and operator documentation

**Files:** Create `deploy/openshift.yaml` and `README.md`.

- [x] Add non-root Deployment, ServiceAccount, ClusterRole/Binding (only `get/list/watch` on the three specified resources), Service, and Route.
- [x] Document Azure URL/branch/path env vars, Azure token and registry Docker config Secret mounts, 5-minute cache defaults, required Argo CRDs, and the cluster-wide read scope.
- [x] Document that ingress/network policy must enforce internal access because the app has no sign-in.
- [x] Check YAML structure and build the container image with `docker build` when Docker is available.
