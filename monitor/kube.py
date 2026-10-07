"""Kubernetes access without a client library: in-cluster REST calls, and
list+watch informers that keep resources in memory, one thread per
(namespace, resource). Objects are plain dicts decoded from JSON; they are
shared with the informers and must be treated as read-only."""

import http.client
import json
import logging
import os
import socket
import ssl
import threading
import time
from contextlib import contextmanager
from datetime import UTC, datetime
from typing import Any, Callable, NamedTuple
from urllib.parse import urlencode

from .config import GVR

log = logging.getLogger(__name__)

SA_DIR = "/var/run/secrets/kubernetes.io/serviceaccount"
RECONCILE_INTERVAL = 2.0
STATUS_LOG_INTERVAL = 30.0
# The server ends each watch after this; the informer resumes from its resourceVersion.
WATCH_TIMEOUT = 300
LIST_PAGE_SIZE = 500
LAST_APPLIED_ANNO = "kubectl.kubernetes.io/last-applied-configuration"

# Core v1 resources, which unlike the CRDs never change version.
PODS = GVR("", "v1", "pods")
NAMESPACES = GVR("", "v1", "namespaces")

# A Watcher whose namespaces are ALL_NAMESPACES lists/watches its resources
# cluster-wide with one informer per resource.
ALL_NAMESPACES = lambda: [""]  # noqa: E731


# --- Object accessors (the equivalents of unstructured.Nested*) ---


def nested(obj: Any, *path: str) -> Any:
    for key in path:
        if not isinstance(obj, dict):
            return None
        obj = obj.get(key)
    return obj


def nested_str(obj: Any, *path: str) -> str:
    value = nested(obj, *path)
    return value if isinstance(value, str) else ""


def nested_list(obj: Any, *path: str) -> list:
    value = nested(obj, *path)
    return value if isinstance(value, list) else []


def nested_int(obj: Any, *path: str) -> int | None:
    value = nested(obj, *path)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    return int(value)


def parse_time(value: Any) -> datetime | None:
    """Reads an RFC 3339 timestamp; missing or malformed values are None."""
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = datetime.fromisoformat(value)
    except ValueError:
        return None
    return parsed.astimezone(UTC) if parsed.tzinfo else None


def nested_time(obj: Any, *path: str) -> datetime | None:
    return parse_time(nested(obj, *path))


def name(obj: dict) -> str:
    return nested_str(obj, "metadata", "name")


def namespace(obj: dict) -> str:
    return nested_str(obj, "metadata", "namespace")


def labels(obj: dict) -> dict:
    return nested(obj, "metadata", "labels") or {}


def annotations(obj: dict) -> dict:
    return nested(obj, "metadata", "annotations") or {}


def creation_time(obj: dict) -> datetime | None:
    return nested_time(obj, "metadata", "creationTimestamp")


def key(ns: str, obj_name: str) -> str:
    """Identifies a namespaced object."""
    return f"{ns}/{obj_name}"


def omitempty(**fields: Any) -> dict:
    """Drops None, False, "" and empty collections, like Go's `json:",omitempty"`;
    a zero number is kept."""
    return {k: v for k, v in fields.items() if v is not None and v is not False and v != "" and v != [] and v != {}}


# --- Pods ---


def pod_view(pod: dict) -> dict:
    """Summarises a pod: phase, ready containers, restarts and the state of every container."""
    statuses = [s for s in nested_list(pod, "status", "containerStatuses") if isinstance(s, dict)]
    return {
        "name": name(pod),
        "phase": nested_str(pod, "status", "phase") or "Unknown",
        "ready": sum(1 for s in statuses if s.get("ready") is True),
        "containers": len(nested_list(pod, "status", "containerStatuses")),
        "restarts": sum(nested_int(s, "restartCount") or 0 for s in statuses),
        **omitempty(
            node=nested_str(pod, "spec", "nodeName"),
            startedAt=nested_time(pod, "status", "startTime"),
            containerStates=_container_states(pod),
        ),
    }


def _container_states(pod: dict) -> list[str]:
    states = []
    for field in ("containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"):
        for status in nested_list(pod, "status", field):
            if not isinstance(status, dict) or not isinstance(status.get("state"), dict):
                continue
            for state_name in ("waiting", "running", "terminated"):
                if state_name not in status["state"]:
                    continue
                label = state_name.capitalize()
                if reason := nested_str(status["state"][state_name], "reason"):
                    label += f" ({reason})"
                states.append(f"{nested_str(status, 'name')}: {label}")
                break
    return sorted(states)


# --- REST client ---


class APIError(Exception):
    def __init__(self, status: int, message: str):
        super().__init__(f"HTTP {status}: {message}")
        self.status = status


def resource_path(gvr: GVR, ns: str = "") -> str:
    base = f"/apis/{gvr.group}/{gvr.version}" if gvr.group else f"/api/{gvr.version}"
    return f"{base}/namespaces/{ns}/{gvr.resource}" if ns else f"{base}/{gvr.resource}"


class API:
    """In-cluster API server access with the pod's ServiceAccount."""

    def __init__(self):
        host, port = os.environ.get("KUBERNETES_SERVICE_HOST"), os.environ.get("KUBERNETES_SERVICE_PORT")
        if not host or not port:
            raise RuntimeError("load in-cluster Kubernetes config: KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be set")
        self.host, self.port = host, int(port)
        self.ssl = ssl.create_default_context(cafile=f"{SA_DIR}/ca.crt")

    def _open(self, path: str, timeout: float) -> tuple[http.client.HTTPSConnection, http.client.HTTPResponse]:
        with open(f"{SA_DIR}/token") as file:  # re-read: bound tokens rotate
            token = file.read().strip()
        conn = http.client.HTTPSConnection(self.host, self.port, context=self.ssl, timeout=timeout)
        try:
            conn.request("GET", path, headers={"Authorization": f"Bearer {token}", "Accept": "application/json"})
            resp = conn.getresponse()
            if resp.status != 200:
                body = resp.read(64 << 10)
                try:
                    message = json.loads(body).get("message") or resp.reason
                except (ValueError, AttributeError):
                    message = body.decode(errors="replace") or resp.reason
                raise APIError(resp.status, message)
        except BaseException:
            conn.close()
            raise
        return conn, resp

    def get(self, path: str, timeout: float = 60) -> dict:
        conn, resp = self._open(path, timeout)
        try:
            return json.load(resp)
        finally:
            conn.close()

    @contextmanager
    def stream(self, path: str, timeout: float):
        """Yields (connection, response) for reading a watch line by line."""
        conn, resp = self._open(path, timeout)
        try:
            yield conn, resp
        finally:
            conn.close()


def served_resources(api, resources: list[GVR]) -> tuple[list[GVR], list[str]]:
    """Splits resources into those the API server serves and the
    "group/version/resource" names of those it does not. Discovery failures
    other than "not found" keep the resource so a transient error hides nothing."""
    served, missing = [], []
    for gvr in resources:
        base = f"/apis/{gvr.group}/{gvr.version}" if gvr.group else f"/api/{gvr.version}"
        try:
            names = {item.get("name") for item in api.get(base).get("resources") or []}
        except Exception as error:  # noqa: BLE001
            if isinstance(error, APIError) and error.status == 404:
                missing.append("/".join(gvr))
            else:
                log.info("resource discovery failed, watching anyway: resource=%s error=%s", "/".join(gvr), error)
                served.append(gvr)
            continue
        if gvr.resource in names:
            served.append(gvr)
        else:
            missing.append("/".join(gvr))
    return served, missing


# --- Informers ---


def _trim(obj: dict) -> dict:
    """Dropping bulky metadata keeps the cache small; the dashboard never reads it."""
    metadata = obj.get("metadata")
    if isinstance(metadata, dict):
        metadata.pop("managedFields", None)
        if isinstance(metadata.get("annotations"), dict):
            metadata["annotations"].pop(LAST_APPLIED_ANNO, None)
    return obj


def _display(ns: str) -> str:
    return ns or "*"


class Informer:
    """Keeps one resource of one namespace ("" = all) in memory with list+watch."""

    def __init__(self, api, gvr: GVR, ns: str, owner: str):
        self.api, self.gvr, self.ns, self.owner = api, gvr, ns, owner
        self.path = resource_path(gvr, ns)
        self.synced = False
        self.error = ""  # current list/watch error, "" when healthy
        self.logged = False  # sync already logged
        self._items: dict[str, dict] = {}
        self._lock = threading.Lock()
        self._stopped = threading.Event()
        self._conn = None

    def start(self):
        threading.Thread(target=self._run, name=f"{self.owner}:{_display(self.ns)}/{self.gvr.resource}", daemon=True).start()

    def stop(self):
        self._stopped.set()
        conn = self._conn
        if conn is not None and conn.sock is not None:
            try:  # wakes the thread blocked reading the watch
                conn.sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass

    def list(self) -> list[dict]:
        with self._lock:
            return list(self._items.values())

    def __len__(self) -> int:
        return len(self._items)

    def _set_error(self, message: str):
        if message == self.error:
            return
        if message:
            log.info("%s watch error: namespace=%r resource=%r error=%r", self.owner, self.ns, self.gvr.resource, message)
        else:
            log.info("%s watch recovered: namespace=%r resource=%r", self.owner, self.ns, self.gvr.resource)
        self.error = message

    def _run(self):
        backoff = 1.0
        while not self._stopped.is_set():
            try:
                version = self._list()
                self._set_error("")
                backoff = 1.0
                while version is not None and not self._stopped.is_set():
                    version = self._watch(version)
            except Exception as error:  # noqa: BLE001 - every failure is retried
                if self._stopped.is_set():
                    return
                self._set_error(str(error) or type(error).__name__)
                self._stopped.wait(backoff)
                backoff = min(backoff * 2, 30.0)

    def _list(self) -> str:
        items, token = {}, ""
        while True:
            params = {"limit": LIST_PAGE_SIZE} | ({"continue": token} if token else {})
            page = self.api.get(f"{self.path}?{urlencode(params)}")
            # List items carry no kind/apiVersion; restore them like client-go does.
            kind, api_version = (page.get("kind") or "").removesuffix("List"), page.get("apiVersion")
            for obj in page.get("items") or []:
                obj["kind"], obj["apiVersion"] = kind, api_version
                items[key(namespace(obj), name(obj))] = _trim(obj)
            token = nested_str(page, "metadata", "continue")
            if not token:
                break
        with self._lock:
            self._items = items
        self.synced = True
        return nested_str(page, "metadata", "resourceVersion")

    def _watch(self, version: str) -> str | None:
        """Applies watch events until the server ends the watch; returns the
        resourceVersion to resume from, or None when a relist is needed."""
        params = {"watch": "1", "resourceVersion": version, "allowWatchBookmarks": "true", "timeoutSeconds": WATCH_TIMEOUT}
        with self.api.stream(f"{self.path}?{urlencode(params)}", timeout=WATCH_TIMEOUT + 60) as (conn, resp):
            self._conn = conn
            for line in resp:
                if self._stopped.is_set():
                    return version
                if not line.strip():
                    continue
                event = json.loads(line)
                kind, obj = event.get("type"), event.get("object") or {}
                if kind == "ERROR":
                    if obj.get("code") == 410:  # resourceVersion too old
                        return None
                    raise APIError(obj.get("code") or 0, obj.get("message") or "watch error")
                version = nested_str(obj, "metadata", "resourceVersion") or version
                if kind == "BOOKMARK":
                    continue
                object_key = key(namespace(obj), name(obj))
                with self._lock:
                    if kind == "DELETED":
                        self._items.pop(object_key, None)
                    else:
                        self._items[object_key] = _trim(obj)
                self._set_error("")
        return version


class Snapshot(NamedTuple):
    objects: dict[GVR, list[dict]]
    health: dict


class Watcher:
    """Keeps informers for a fixed set of resources across a namespace set
    that may change over time."""

    def __init__(self, name: str, api, resources: list[GVR], namespaces: Callable[[], list[str]]):
        # Only resources the API server serves are watched. A configured resource
        # whose CRD is not installed (or has another version) is skipped and
        # reported in the health message instead of failing forever.
        self.resources, self.missing = served_resources(api, resources)
        for resource in self.missing:
            log.info("%s watcher skipping resource not served by the cluster: %s (check the CRD and its version in the ConfigMap; restart after installing it)", name, resource)
        self.name, self.api, self.namespaces = name, api, namespaces
        self._watches: dict[str, dict[GVR, Informer]] = {}
        self._lock = threading.Lock()
        self._last_success: datetime | None = None
        self._was_ready = False
        self._last_status_log = 0.0

    def run(self, stop: threading.Event):
        log.info("%s watcher starting: resources=%s", self.name, ["/".join(gvr) for gvr in self.resources])
        while True:
            self.reconcile()
            if stop.wait(RECONCILE_INTERVAL):
                with self._lock:
                    for informers in self._watches.values():
                        for informer in informers.values():
                            informer.stop()
                    self._watches.clear()
                return

    def reconcile(self):
        desired = set(self.namespaces())
        started = []
        with self._lock:
            changed = False
            for ns in [ns for ns in self._watches if ns not in desired]:
                log.info("%s watch stopping: namespace=%r", self.name, ns)
                for informer in self._watches.pop(ns).values():
                    informer.stop()
                changed = True
            for ns in desired - self._watches.keys():
                informers = {}
                for gvr in self.resources:
                    log.info("%s watch starting: namespace=%r resource=%r", self.name, ns, "/".join(gvr))
                    informers[gvr] = Informer(self.api, gvr, ns, self.name)
                self._watches[ns] = informers
                started.extend(informers.values())
                changed = True
            for informer in self._informers():
                if informer.synced and not informer.logged:
                    informer.logged = True
                    log.info("%s informer synced: namespace=%r resource=%r objects=%d", self.name, _display(informer.ns), informer.gvr.resource, len(informer))
            synced, errors = self._status()
            if time.monotonic() - self._last_status_log >= STATUS_LOG_INTERVAL:
                counts: dict[str, int] = {}
                for informer in self._informers():
                    counts[informer.gvr.resource] = counts.get(informer.gvr.resource, 0) + len(informer)
                log.info("%s watcher status: namespaces=%d objects=%s synced=%s errors=%s", self.name, len(self._watches), counts, synced, errors)
                self._last_status_log = time.monotonic()
            ready = synced and not errors
            if ready and (not self._was_ready or self._last_success is None or changed):
                self._last_success = datetime.now(UTC)
            self._was_ready = ready
        for informer in started:
            informer.start()

    def snapshot(self) -> Snapshot:
        objects: dict[GVR, list[dict]] = {}
        with self._lock:
            for informer in self._informers():
                objects.setdefault(informer.gvr, []).extend(informer.list())
            synced, errors = self._status()
            health = {"state": "ready", **omitempty(lastSuccess=self._last_success)}
        if errors:
            health |= {"state": "degraded", "error": "; ".join(errors)}
        elif not synced:
            health |= {"state": "syncing", "error": "waiting for informer cache sync"}
        elif self.missing:
            health["error"] = "not served by the cluster, skipped: " + ", ".join(self.missing)
        return Snapshot(objects, health)

    def _informers(self):
        """Must be called with self._lock held."""
        return [informer for informers in self._watches.values() for informer in informers.values()]

    def _status(self) -> tuple[bool, list[str]]:
        """Must be called with self._lock held."""
        informers = self._informers()
        errors = sorted(f"{_display(i.ns)}/{i.gvr.resource}: {i.error}" for i in informers if i.error)
        return all(i.synced for i in informers), errors
