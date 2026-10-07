"""Turns Argo CronWorkflows, their Workflows and pods into dashboard rows and
resolves their project images against the registry."""

from datetime import UTC, datetime, timedelta, tzinfo
from functools import lru_cache
from zoneinfo import ZoneInfo

from croniter import croniter

from .config import parse_duration
from .kube import (
    annotations,
    creation_time,
    key,
    labels,
    name,
    namespace,
    nested,
    nested_list,
    nested_str,
    nested_time,
    omitempty,
    parse_time,
    pod_view,
)

CRON_WORKFLOW_LABEL = "workflows.argoproj.io/cron-workflow"
WORKFLOW_LABEL = "workflows.argoproj.io/workflow"
SCHEDULED_TIME_ANNO = "workflows.argoproj.io/scheduled-time"
_EPOCH = datetime.min.replace(tzinfo=UTC)


def build_views(cron_workflows: list[dict], workflows: list[dict], pods: list[dict], now: datetime) -> list[dict]:
    workflows_by_cron = _group_by_label(workflows, CRON_WORKFLOW_LABEL)
    pods_by_workflow = _group_by_label(pods, WORKFLOW_LABEL)

    views = []
    for cron_workflow in cron_workflows:
        ns, cron_name = namespace(cron_workflow), name(cron_workflow)
        schedules = _cron_schedules(cron_workflow)
        timezone = nested_str(cron_workflow, "spec", "timezone")
        suspended = nested(cron_workflow, "spec", "suspend") is True
        runs = sorted(workflows_by_cron.get(key(ns, cron_name), []), key=_workflow_time, reverse=True)
        next_at, schedule_error = (None, "") if suspended else _next_scheduled_time(schedules, timezone, now)
        views.append({
            "namespace": ns,
            "name": cron_name,
            "schedules": schedules,
            "suspended": suspended,
            "active": bool(nested_list(cron_workflow, "status", "active")),
            "history": [_run_summary(run) for run in runs],  # newest first
            "images": project_images(cron_workflow.get("spec"), ns),
            **omitempty(
                timezone=timezone or "UTC (assumed)",
                lastScheduledAt=nested_time(cron_workflow, "status", "lastScheduledTime"),
                nextScheduledAt=next_at,
                scheduleError=schedule_error,
                lastRun=_workflow_view(runs[0], pods_by_workflow.get(key(ns, name(runs[0])), [])) if runs else None,
            ),
        })
    views.sort(key=lambda view: (view["namespace"], view["name"]))
    return views


def _group_by_label(objects: list[dict], label: str) -> dict[str, list[dict]]:
    groups: dict[str, list[dict]] = {}
    for obj in objects:
        if owner := labels(obj).get(label):
            groups.setdefault(key(namespace(obj), owner), []).append(obj)
    return groups


def project_images(spec, ns: str) -> list[dict]:
    """Every `image:` field of the CronWorkflow spec whose image name contains
    the namespace, wherever the field is (container, script, sidecar, init,
    containerSet or inline templates). Its ID is the part after the last ":"
    (or the digest after "@")."""
    results, seen, ns = [], set(), ns.lower()
    stack = [spec]
    while stack:
        value = stack.pop()
        if isinstance(value, dict):
            for field, item in value.items():
                if field == "image" and isinstance(item, str):
                    image = item.strip()
                    image_name, image_id = split_image(image)
                    if image_id and image not in seen and ns in image_name.lower():
                        seen.add(image)
                        results.append({"reference": image, "imageId": image_id, "status": "unknown"})
                else:
                    stack.append(item)
        elif isinstance(value, list):
            stack.extend(value)
    return sorted(results, key=lambda image: image["reference"])


def split_image(image: str) -> tuple[str, str]:
    """Splits "registry:5000/mlops/bch-proje:ald73" into its name and "ald73";
    a colon before the last "/" is a registry port, not a tag."""
    if "@" in image:
        image_name, _, image_id = image.rpartition("@")
        return image_name, image_id
    colon = image.rfind(":")
    if colon > image.rfind("/"):
        return image[:colon], image[colon + 1:]
    return image, ""


def _run_summary(workflow: dict) -> dict:
    return {
        "name": name(workflow),
        "phase": nested_str(workflow, "status", "phase") or "Pending",
        **omitempty(
            startedAt=nested_time(workflow, "status", "startedAt"),
            finishedAt=nested_time(workflow, "status", "finishedAt"),
        ),
    }


def _workflow_view(workflow: dict, pods: list[dict]) -> dict:
    return {
        **_run_summary(workflow),
        **omitempty(
            createdAt=creation_time(workflow),
            scheduledAt=parse_time(annotations(workflow).get(SCHEDULED_TIME_ANNO)),
        ),
        "pods": [pod_view(pod) for pod in sorted(pods, key=name)],
    }


def _workflow_time(workflow: dict) -> datetime:
    return parse_time(annotations(workflow).get(SCHEDULED_TIME_ANNO)) or creation_time(workflow) or _EPOCH


def _cron_schedules(cron_workflow: dict) -> list[str]:
    schedules = nested(cron_workflow, "spec", "schedules")
    if isinstance(schedules, list) and schedules and all(isinstance(s, str) for s in schedules):
        return schedules
    if schedule := nested_str(cron_workflow, "spec", "schedule"):
        return [schedule]
    return []


def _next_scheduled_time(schedules: list[str], timezone: str, now: datetime) -> tuple[datetime | None, str]:
    if not schedules:
        return None, "no schedule configured"
    try:
        location = ZoneInfo(timezone) if timezone else UTC
    except (KeyError, ValueError, OSError):  # ZoneInfoNotFoundError is a KeyError
        return None, "invalid timezone"
    local_now = now.astimezone(location)
    earliest = None
    for expression in schedules:
        try:
            if expression.startswith("@every "):
                delay = parse_duration(expression.removeprefix("@every ").strip())
                if delay <= 0:
                    raise ValueError(expression)
                upcoming = local_now.replace(microsecond=0) + timedelta(seconds=max(delay, 1))
            else:
                upcoming = _cron_next(expression, location, now.replace(second=0, microsecond=0))
        except (ValueError, KeyError, TypeError):
            return None, "invalid cron expression"
        if earliest is None or upcoming < earliest:
            earliest = upcoming
    if not timezone:
        return earliest, "timezone unset; next run assumes UTC"
    return earliest, ""


@lru_cache(maxsize=8192)
def _cron_next(expression: str, location: tzinfo, minute: datetime) -> datetime:
    """Cron fires on whole minutes, so the next run after any moment equals
    the next run after its minute; CronWorkflows sharing a schedule share it."""
    return croniter(expression, minute.astimezone(location)).get_next(datetime)


def resolve_images(views: list[dict], checker) -> dict:
    """Fills the registry status of every project image in place and returns
    the registry's health as seen by this lookup. The manifest URL uses the
    CronWorkflow's namespace and the image ID."""
    urls = []
    for view in views:
        for image in view["images"]:
            if image.get("imageId"):
                image["url"] = checker.manifest_url(view["namespace"], image["imageId"])
                urls.append(image["url"])
    results = checker.lookup(urls)

    health: dict = {"state": "ready"}
    failed = set()
    for view in views:
        for image in view["images"]:
            result = results.get(image.get("url", ""))
            if result is None:
                continue
            image["status"] = result["status"]
            image.pop("error", None)
            image.pop("checkedAt", None)
            image.update(omitempty(error=result.get("error"), checkedAt=result.get("checkedAt")))
            checked_at = result.get("checkedAt")
            if result["status"] in ("error", "unknown"):
                failed.add(image["url"])
            elif checked_at is not None and ("lastSuccess" not in health or checked_at > health["lastSuccess"]):
                health["lastSuccess"] = checked_at
    if failed:
        health |= {"state": "degraded", "error": f"{len(failed)} image checks failed"}
    return health
