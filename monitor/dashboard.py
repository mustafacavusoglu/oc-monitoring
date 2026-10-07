"""Builds the single JSON snapshot the web UI renders."""

import gzip
import json
import threading
import time
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Callable

from . import batch, serving
from .config import GVR, Config
from .kube import NAMESPACES, PODS, Snapshot, name, namespace, omitempty
from .projects import Snapshot as ProjectSnapshot

# ponytail: one encoded response is shared by every request for this long; it
# caps CPU at one build per second however many viewers poll.
CACHE_SECONDS = 1.0


@dataclass(frozen=True)
class Sources:
    """Read-only views the dashboard combines. Every call is served from
    in-memory caches, so a request never waits on Kubernetes, Azure or Nexus."""

    projects: Callable[[], ProjectSnapshot]
    cluster: Callable[[], Snapshot]  # cluster-wide resources
    pods: Callable[[], Snapshot]  # pods of the watched project namespaces
    images: object  # registry.Checker


def _json_default(value):
    if isinstance(value, datetime):
        return value.isoformat().replace("+00:00", "Z")
    raise TypeError(f"cannot encode {type(value).__name__}")


class Dashboard:
    def __init__(self, sources: Sources, cfg: Config):
        self.sources, self.cfg = sources, cfg
        self._lock = threading.Lock()
        self._built_at = -CACHE_SECONDS
        self._body = self._gzipped = b""

    def response(self) -> tuple[bytes, bytes]:
        """The encoded snapshot as (JSON, gzipped JSON)."""
        with self._lock:  # concurrent requests wait for one build instead of each building
            if time.monotonic() - self._built_at >= CACHE_SECONDS:
                self._body = json.dumps(self.snapshot(datetime.now(UTC)), default=_json_default, separators=(",", ":")).encode()
                self._gzipped = gzip.compress(self._body, compresslevel=6)
                self._built_at = time.monotonic()
            return self._body, self._gzipped

    def snapshot(self, now: datetime) -> dict:
        project_snapshot = self.sources.projects()
        cluster_snapshot = self.sources.cluster()
        pod_snapshot = self.sources.pods()
        objects, pods = cluster_snapshot.objects, pod_snapshot.objects.get(PODS, [])
        res = self.cfg.resources

        # Everything comes from the cluster; the project file only names namespaces.
        cron_workflows = batch.build_views(objects.get(res.cron_workflows, []), objects.get(res.workflows, []), pods, now)
        models = serving.build_models(
            objects.get(res.inference_services, []),
            objects.get(res.serving_runtimes, []),
            objects.get(res.llm_inference_services, []),
            pods,
            self.cfg.serving,
        )

        project_health = {"state": "ready", **omitempty(lastSuccess=project_snapshot.last_success)}
        if project_snapshot.stale:
            project_health |= {"state": "degraded", "error": project_snapshot.error}
        elif project_snapshot.skipped:
            project_health["error"] = f"{len(project_snapshot.skipped)} project entries skipped: {'; '.join(project_snapshot.skipped)}"

        return {
            "generatedAt": now,
            "console": self._console(),
            "refreshIntervalSeconds": int(self.cfg.ui_refresh_interval),
            "sources": {
                "projects": project_health,
                "cluster": cluster_snapshot.health,
                "pods": pod_snapshot.health,
                "registry": batch.resolve_images(cron_workflows, self.sources.images),
            },
            "namespaces": sorted({*project_snapshot.namespaces(), *(m["namespace"] for m in models), *(w["namespace"] for w in cron_workflows)}),
            "projects": _coverage(project_snapshot.projects, objects, res, pods),
            "models": models,
            "cronWorkflows": cron_workflows,
        }

    def _console(self) -> dict:
        res = self.cfg.resources

        def ref(gvr: GVR, kind: str) -> str:
            return f"{gvr.group}~{gvr.version}~{kind}"

        return {"url": self.cfg.console_url, "refs": {
            "CronWorkflow": ref(res.cron_workflows, "CronWorkflow"),
            "Workflow": ref(res.workflows, "Workflow"),
            "InferenceService": ref(res.inference_services, "InferenceService"),
            "ServingRuntime": ref(res.serving_runtimes, "ServingRuntime"),
            "LLMInferenceService": ref(res.llm_inference_services, "LLMInferenceService"),
        }}


def _coverage(source: list[dict], objects: dict[GVR, list[dict]], res, pods: list[dict]) -> list[dict]:
    """Adds to every project what the cluster holds in its namespace, so a
    project with no resources (or no namespace) is visible."""
    def count(items: list[dict]) -> dict[str, int]:
        counts: dict[str, int] = {}
        for item in items:
            counts[namespace(item)] = counts.get(namespace(item), 0) + 1
        return counts

    existing = {name(ns) for ns in objects.get(NAMESPACES, [])}
    cron_workflows, inference_services, pod_counts = count(objects.get(res.cron_workflows, [])), count(objects.get(res.inference_services, [])), count(pods)
    return [{
        **project,
        "namespaceExists": project["namespace"] in existing,
        "cronWorkflows": cron_workflows.get(project["namespace"], 0),
        "inferenceServices": inference_services.get(project["namespace"], 0),
        "pods": pod_counts.get(project["namespace"], 0),
    } for project in source]
