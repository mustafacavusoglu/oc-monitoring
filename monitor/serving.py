"""Classifies KServe deployments into LLM and ML models.

An LLMInferenceService is always an LLM. An InferenceService is classified by
the images of the ServingRuntime it names: a configured LLM keyword (e.g.
vllm) makes it an LLM, a configured ML keyword (e.g. triton) an ML model, any
other runtime a Custom Serve model. Every InferenceService carries its pods."""

import math
import re
from decimal import Decimal, InvalidOperation

from .config import ServingRules
from .kube import (
    creation_time,
    key,
    labels,
    name,
    namespace,
    nested,
    nested_int,
    nested_list,
    nested_str,
    nested_time,
    omitempty,
    pod_view,
)

INFERENCE_SERVICE_LABEL = "serving.kserve.io/inferenceservice"


def build_models(inference_services: list[dict], serving_runtimes: list[dict], llm_inference_services: list[dict], pods: list[dict], rules: ServingRules) -> list[dict]:
    runtimes = {key(namespace(runtime), name(runtime)): runtime for runtime in serving_runtimes}
    pods_by_model: dict[str, list[dict]] = {}
    for pod in pods:
        if owner := labels(pod).get(INFERENCE_SERVICE_LABEL):
            pods_by_model.setdefault(key(namespace(pod), owner), []).append(pod)

    models = []
    for isvc in inference_services:
        model = _inference_service_model(isvc, runtimes, rules)
        pod_views = [pod_view(pod) for pod in pods_by_model.get(key(model["namespace"], model["name"]), [])]
        if pod_views:
            model["pods"] = sorted(pod_views, key=lambda pod: pod["name"])
        models.append(model)
    models.extend(_llm_inference_service_model(llmisvc, rules) for llmisvc in llm_inference_services)
    models.sort(key=lambda model: (model["namespace"], model["name"]))
    return models


def _inference_service_model(isvc: dict, runtimes: dict[str, dict], rules: ServingRules) -> dict:
    """Joins an InferenceService with the ServingRuntime its
    spec.predictor.model.runtime names: the runtime's images decide the type,
    and its container resources are used when the InferenceService sets no GPU/MIG."""
    runtime_name = nested_str(isvc, "spec", "predictor", "model", "runtime")
    runtime = runtimes.get(key(namespace(isvc), runtime_name))
    images = _container_images(runtime, "spec", "containers") if runtime else []
    model = _base_model(isvc, classify(images, rules))
    model.update(omitempty(
        runtime=runtime_name,
        runtimeMissing=bool(runtime_name) and runtime is None,
        images=images,
        modelFormat=nested_str(isvc, "spec", "predictor", "model", "modelFormat", "name"),
        storageUri=nested_str(isvc, "spec", "predictor", "model", "storageUri"),
        minReplicas=nested_int(isvc, "spec", "predictor", "minReplicas"),
        maxReplicas=nested_int(isvc, "spec", "predictor", "maxReplicas"),
    ))
    _add_accelerators(model, nested(isvc, "spec", "predictor", "model", "resources"), rules)
    if runtime and model["gpu"] == 0 and "mig" not in model:
        for container in nested_list(runtime, "spec", "containers"):
            _add_accelerators(model, nested(container, "resources"), rules)
    return model


def _llm_inference_service_model(llmisvc: dict, rules: ServingRules) -> dict:
    model = _base_model(llmisvc, "llm")
    replicas = nested_int(llmisvc, "spec", "replicas")
    model.update(omitempty(
        images=_container_images(llmisvc, "spec", "template", "containers"),
        modelFormat=nested_str(llmisvc, "spec", "model", "name"),
        storageUri=nested_str(llmisvc, "spec", "model", "uri"),
        minReplicas=replicas,
        maxReplicas=replicas,
    ))
    for container in nested_list(llmisvc, "spec", "template", "containers"):
        _add_accelerators(model, nested(container, "resources"), rules)
    return model


def _base_model(obj: dict, model_type: str) -> dict:
    model = {
        "namespace": namespace(obj),
        "name": name(obj),
        "kind": nested_str(obj, "kind"),
        "type": model_type,
        "state": "Unknown",
        "gpu": 0,
        **omitempty(createdAt=creation_time(obj), url=nested_str(obj, "status", "url")),
    }
    for condition in nested_list(obj, "status", "conditions"):
        if not isinstance(condition, dict) or condition.get("type") != "Ready":
            continue
        model["state"] = {"True": "Ready", "False": "NotReady"}.get(condition.get("status"), model["state"])
        for field in ("reason", "message", "stateSince"):
            model.pop(field, None)
        model.update(omitempty(
            reason=nested_str(condition, "reason"),
            message=nested_str(condition, "message"),
            stateSince=nested_time(condition, "lastTransitionTime"),
        ))
    return model


def classify(images: list[str], rules: ServingRules) -> str:
    """LLM keywords are checked first so a runtime image matching both is an
    LLM; a runtime matching neither (or not found) is Custom Serve."""
    if _matches_any(images, rules.llm_image_keywords):
        return "llm"
    if _matches_any(images, rules.ml_image_keywords):
        return "ml"
    return "custom"


def _matches_any(images: list[str], keywords: list[str]) -> bool:
    return any(keyword.lower() in image.lower() for image in images for keyword in keywords)


def _container_images(obj: dict, *path: str) -> list[str]:
    return [image for container in nested_list(obj, *path) if (image := nested_str(container, "image"))]


def _add_accelerators(model: dict, resources, rules: ServingRules):
    """Adds the full-GPU and MIG-slice counts of a resources block to the
    model. MIG resources (e.g. nvidia.com/mig-1g.5gb) are keyed by profile."""
    for resource, count in _resource_counts(resources).items():
        if resource == rules.gpu_resource_name:
            model["gpu"] += count
        elif resource.startswith(rules.mig_resource_prefix):
            mig = model.setdefault("mig", {})
            profile = resource.removeprefix(rules.mig_resource_prefix)
            mig[profile] = mig.get(profile, 0) + count


def _resource_counts(resources) -> dict[str, int]:
    """Every resource quantity of a resources block; a limit overrides the
    request of the same resource."""
    counts = {}
    for kind in ("requests", "limits"):
        quantities = nested(resources, kind)
        if isinstance(quantities, dict):
            for resource, value in quantities.items():
                if (count := quantity_value(value)) is not None:
                    counts[resource] = count
    return counts


_QUANTITY = re.compile(r"([+-]?(?:\d+\.?\d*|\.\d+))(?:([eE][+-]?\d+)|(Ki|Mi|Gi|Ti|Pi|Ei|[numkMGTPE]))?")
_SUFFIXES = {
    "": 1, "n": Decimal("1e-9"), "u": Decimal("1e-6"), "m": Decimal("1e-3"),
    "k": 10**3, "M": 10**6, "G": 10**9, "T": 10**12, "P": 10**15, "E": 10**18,
    "Ki": 2**10, "Mi": 2**20, "Gi": 2**30, "Ti": 2**40, "Pi": 2**50, "Ei": 2**60,
}


def quantity_value(value) -> int | None:
    """A Kubernetes resource quantity ("2", "500m", "1Gi", 1) rounded up to an
    integer, like resource.Quantity.Value(); invalid quantities are None."""
    if isinstance(value, bool):
        return None
    match = _QUANTITY.fullmatch(str(value))
    if not match:
        return None
    try:
        number = Decimal(match[1] + (match[2] or "")) * _SUFFIXES[match[3] or ""]
    except InvalidOperation:
        return None
    return math.ceil(number)
