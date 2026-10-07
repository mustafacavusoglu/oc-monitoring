"""Reads the project JSON from Azure Repos; its keys name the project namespaces."""

import base64
import dataclasses
import json
import logging
import threading
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from datetime import UTC, datetime
from urllib.parse import quote, urlencode, urlsplit

from .config import Config

log = logging.getLogger(__name__)

MAX_PROJECT_FILE_SIZE = 4 << 20


@dataclass(frozen=True)
class Snapshot:
    # Every project in the project JSON: {"key", "namespace"}.
    projects: list[dict] = field(default_factory=list)
    # Project entries that could not be read.
    skipped: list[str] = field(default_factory=list)
    last_success: datetime | None = None
    stale: bool = False
    error: str = ""

    def namespaces(self) -> list[str]:
        return [project["namespace"] for project in self.projects]


class Source:
    def __init__(self, cfg: Config, opener=urllib.request.urlopen):
        self.cfg, self.opener = cfg, opener
        # Replaced as a whole, never mutated, so readers need no lock.
        self._snapshot = Snapshot(stale=True, error="waiting for first project refresh")

    def run(self, stop: threading.Event):
        while True:
            self.refresh()
            if stop.wait(self.cfg.project_refresh_interval):
                return

    def snapshot(self) -> Snapshot:
        return self._snapshot

    def refresh(self):
        try:
            raw = self._fetch()
        except Exception as error:  # noqa: BLE001 - the last good list stays in use
            log.info("project source refresh failed: %s", error)
            self._snapshot = dataclasses.replace(self._snapshot, stale=True, error=str(error))
            return
        projects, skipped = parse_projects(raw)
        log.info("project source refresh completed: entries=%d namespaces=%d skipped=%d", len(raw), len(projects), len(skipped))
        for reason in skipped:
            log.info("project source skipped entry: %s", reason)
        self._snapshot = Snapshot(projects=projects, skipped=skipped, last_success=datetime.now(UTC))

    def _fetch(self) -> dict:
        cfg = self.cfg
        request = urllib.request.Request(azure_item_url(cfg.azure_repo_url, cfg.azure_projects_path, cfg.azure_repo_branch), headers={"Accept": "application/octet-stream"})
        if token := cfg.azure_token or (_read_secret_file(cfg.azure_token_file) if cfg.azure_token_file else ""):
            request.add_header("Authorization", "Basic " + base64.b64encode(f":{token}".encode()).decode())
        try:
            with self.opener(request, timeout=cfg.upstream_timeout) as resp:
                if resp.status != 200:
                    raise RuntimeError(f"fetch project JSON from Azure Repos: HTTP {resp.status}")
                body = resp.read(MAX_PROJECT_FILE_SIZE + 1)
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"fetch project JSON from Azure Repos: HTTP {error.code}") from None
        except urllib.error.URLError as error:
            raise RuntimeError(f"fetch project JSON from Azure Repos: {error.reason}") from None
        if len(body) > MAX_PROJECT_FILE_SIZE:
            raise RuntimeError(f"project JSON exceeds {MAX_PROJECT_FILE_SIZE} bytes")
        try:
            projects = json.loads(body)
        except ValueError as error:
            raise RuntimeError(f"parse project JSON: {error}") from None
        if not isinstance(projects, dict):
            raise RuntimeError("project JSON must be a top-level object")
        return projects


def parse_projects(raw: dict) -> tuple[list[dict], list[str]]:
    """Turns every project key into its namespace (PROJECT_NAME → project-name).
    Only the keys matter; everything else comes from the cluster. An empty key
    or a second key for the same namespace is skipped and reported."""
    projects, skipped, owners = [], [], {}
    for project_key in sorted(raw):
        ns = project_key.strip().replace("_", "-").lower()
        if not ns:
            skipped.append("empty project key")
        elif ns in owners:
            skipped.append(f"{json.dumps(project_key)} maps to namespace {json.dumps(ns)} already used by {json.dumps(owners[ns])}")
        else:
            owners[ns] = project_key
            projects.append({"key": project_key, "namespace": ns})
    return projects, skipped


def azure_item_url(repo_url: str, file_path: str, branch: str) -> str:
    repo = urlsplit(repo_url)
    if repo.scheme != "https" or not repo.hostname or repo.query or repo.fragment:
        raise RuntimeError("AZURE_REPO_URL must be an HTTPS Azure Repos repository URL")
    segments = repo.path.strip("/").split("/")
    git_index = segments.index("_git") if "_git" in segments else -1
    if git_index < 1 or git_index + 1 >= len(segments):
        raise RuntimeError("AZURE_REPO_URL must contain /_git/{repository}")
    repository = segments[git_index + 1]
    if not repository:
        raise RuntimeError("invalid repository name in AZURE_REPO_URL")
    path = "/".join(segments[:git_index] + ["_apis/git/repositories", repository, "items"])
    query = urlencode(sorted({
        "path": "/" + file_path.strip().lstrip("/"),
        "versionDescriptor.version": branch,
        "versionDescriptor.versionType": "branch",
        "download": "true",
        "api-version": "7.1",
    }.items()))
    return f"https://{repo.netloc}/{quote(path)}?{query}"


def _read_secret_file(file: str) -> str:
    try:
        with open(file) as handle:
            token = handle.read().strip()
    except OSError as error:
        raise RuntimeError(f"read Azure token file: {error}") from None
    if not token:
        raise RuntimeError("Azure token file is empty")
    return token
