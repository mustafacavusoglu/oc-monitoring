"""Loads every environment-specific setting from the process environment,
which the Deployment fills from a ConfigMap (and an optional Secret). Nothing
here has an in-code default: a missing key is a startup error."""

import os
import re
from dataclasses import dataclass
from typing import NamedTuple


class GVR(NamedTuple):
    group: str
    version: str
    resource: str


@dataclass(frozen=True)
class ServingRules:
    """Classifies KServe InferenceServices by their ServingRuntime image."""

    llm_image_keywords: list[str]
    ml_image_keywords: list[str]
    gpu_resource_name: str
    # Matches MIG slice resources; the rest of the name is the profile.
    mig_resource_prefix: str


@dataclass(frozen=True)
class Resources:
    """API group/version/resource of each watched CRD, configured per cluster."""

    cron_workflows: GVR
    workflows: GVR
    inference_services: GVR
    serving_runtimes: GVR
    llm_inference_services: GVR


@dataclass(frozen=True)
class Config:
    http_addr: str
    web_dir: str
    azure_repo_url: str
    azure_repo_branch: str
    azure_projects_path: str
    azure_token: str  # optional, from Secret
    azure_token_file: str  # optional, from Secret mount
    project_refresh_interval: float  # durations are seconds
    # Contains {namespace} and {imageId} placeholders.
    nexus_manifest_url_template: str
    image_cache_ttl: float
    registry_check_concurrency: int
    upstream_timeout: float
    serving: ServingRules
    resources: Resources
    ui_refresh_interval: float
    # The OpenShift web console the UI links resources to.
    console_url: str


_DURATION_PART = re.compile(r"(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)")
_DURATION_UNITS = {"ns": 1e-9, "us": 1e-6, "µs": 1e-6, "μs": 1e-6, "ms": 1e-3, "s": 1, "m": 60, "h": 3600}


def parse_duration(value: str) -> float:
    """Go's time.ParseDuration ("1h30m", "300ms", "-2s") in seconds."""
    text = value[1:] if value[:1] in ("+", "-") else value
    if text == "0":
        return 0.0
    parts = _DURATION_PART.findall(text)
    if not parts or "".join(number + unit for number, unit in parts) != text:
        raise ValueError(f"invalid duration {value!r}")
    seconds = sum(float(number) * _DURATION_UNITS[unit] for number, unit in parts)
    return -seconds if value.startswith("-") else seconds


class _Env:
    """Collects every missing or invalid key so one startup error lists them all."""

    def __init__(self):
        self.missing: list[str] = []
        self.problems: list[str] = []

    def str(self, key: str) -> str:
        value = os.environ.get(key, "").strip()
        if not value:
            self.missing.append(key)
        return value

    def duration(self, key: str) -> float:
        value = self.str(key)
        if not value:
            return 0.0
        try:
            seconds = parse_duration(value)
        except ValueError:
            seconds = 0.0
        if seconds <= 0:
            self.problems.append(f"{key} must be a positive duration, got {value!r}")
        return seconds

    def positive_int(self, key: str) -> int:
        value = self.str(key)
        if not value:
            return 0
        try:
            number = int(value)
        except ValueError:
            number = 0
        if number < 1:
            self.problems.append(f"{key} must be a positive integer, got {value!r}")
        return number

    def list(self, key: str) -> list[str]:
        return [item.strip() for item in self.str(key).split(",") if item.strip()]

    def resource(self, key: str) -> GVR:
        """Parses "group/version/resource"; the core group is written "/v1/pods"."""
        value = self.str(key)
        if not value:
            return GVR("", "", "")
        parts = value.split("/")
        if len(parts) != 3 or not parts[1] or not parts[2]:
            self.problems.append(f"{key} must look like group/version/resource, got {value!r}")
            return GVR("", "", "")
        return GVR(*parts)

    def error(self) -> str:
        errors = [f"missing required settings: {', '.join(self.missing)}"] if self.missing else []
        return "\n".join(errors + self.problems)


def load() -> Config:
    e = _Env()
    cfg = Config(
        http_addr=e.str("HTTP_ADDR"),
        web_dir=e.str("WEB_DIR"),
        azure_repo_url=e.str("AZURE_REPO_URL"),
        azure_repo_branch=e.str("AZURE_REPO_BRANCH"),
        azure_projects_path=e.str("AZURE_PROJECTS_PATH"),
        azure_token=os.environ.get("AZURE_TOKEN", "").strip(),
        azure_token_file=os.environ.get("AZURE_TOKEN_FILE", "").strip(),
        project_refresh_interval=e.duration("PROJECT_REFRESH_INTERVAL"),
        nexus_manifest_url_template=e.str("NEXUS_MANIFEST_URL_TEMPLATE"),
        image_cache_ttl=e.duration("IMAGE_CACHE_TTL"),
        registry_check_concurrency=e.positive_int("REGISTRY_CHECK_CONCURRENCY"),
        upstream_timeout=e.duration("UPSTREAM_TIMEOUT"),
        serving=ServingRules(
            llm_image_keywords=e.list("LLM_RUNTIME_IMAGE_KEYWORDS"),
            ml_image_keywords=e.list("ML_RUNTIME_IMAGE_KEYWORDS"),
            gpu_resource_name=e.str("GPU_RESOURCE_NAME"),
            mig_resource_prefix=e.str("MIG_RESOURCE_PREFIX"),
        ),
        resources=Resources(
            cron_workflows=e.resource("CRONWORKFLOW_RESOURCE"),
            workflows=e.resource("WORKFLOW_RESOURCE"),
            inference_services=e.resource("INFERENCE_SERVICE_RESOURCE"),
            serving_runtimes=e.resource("SERVING_RUNTIME_RESOURCE"),
            llm_inference_services=e.resource("LLM_INFERENCE_SERVICE_RESOURCE"),
        ),
        ui_refresh_interval=e.duration("UI_REFRESH_INTERVAL"),
        console_url=e.str("OPENSHIFT_CONSOLE_URL").rstrip("/"),
    )
    for placeholder in ("{namespace}", "{imageId}"):
        if cfg.nexus_manifest_url_template and placeholder not in cfg.nexus_manifest_url_template:
            e.problems.append(f"NEXUS_MANIFEST_URL_TEMPLATE must contain {placeholder}")
    if error := e.error():
        raise ValueError(error)
    return cfg
