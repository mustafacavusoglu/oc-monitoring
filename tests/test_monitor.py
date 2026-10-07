"""Ports of the Go test suite plus an informer check. Run: python -m unittest -v"""

import gzip
import io
import json
import os
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from contextlib import contextmanager
from datetime import UTC, datetime
from http.server import ThreadingHTTPServer
from unittest import mock

from monitor import batch, config, dashboard, kube, projects, registry, serving
from monitor.config import GVR, Config, Resources, ServingRules
from monitor.kube import NAMESPACES, Snapshot
from monitor.server import make_handler

TESTDATA = os.path.join(os.path.dirname(__file__), "testdata")


def load(path: str):
    with open(os.path.join(TESTDATA, path)) as file:
        return json.load(file)


RESOURCES = Resources(
    cron_workflows=GVR("argoproj.io", "v1alpha1", "cronworkflows"),
    workflows=GVR("argoproj.io", "v1alpha1", "workflows"),
    inference_services=GVR("serving.kserve.io", "v1beta1", "inferenceservices"),
    serving_runtimes=GVR("serving.kserve.io", "v1alpha1", "servingruntimes"),
    llm_inference_services=GVR("serving.kserve.io", "v1alpha1", "llminferenceservices"),
)
RULES = ServingRules(["vllm"], ["triton"], "nvidia.com/gpu", "nvidia.com/mig-")


def make_config(**overrides) -> Config:
    fields = dict(
        http_addr=":8080", web_dir="", azure_repo_url="", azure_repo_branch="main", azure_projects_path="projects.json",
        azure_token="", azure_token_file="", project_refresh_interval=300, nexus_manifest_url_template="",
        image_cache_ttl=3600, registry_check_concurrency=1, upstream_timeout=1, serving=RULES, resources=RESOURCES,
        ui_refresh_interval=30, console_url="",
    )
    return Config(**(fields | overrides))


VALID_ENV = {
    "HTTP_ADDR": ":8080",
    "WEB_DIR": "web/dist",
    "AZURE_REPO_URL": "https://dev.azure.com/example/project/_git/repo",
    "AZURE_REPO_BRANCH": "main",
    "AZURE_PROJECTS_PATH": "/projects.json",
    "PROJECT_REFRESH_INTERVAL": "5m",
    "NEXUS_MANIFEST_URL_TEMPLATE": "https://nexus.example.test/v2/mlops/bch-{namespace}/manifests/{imageId}",
    "IMAGE_CACHE_TTL": "24h",
    "REGISTRY_CHECK_CONCURRENCY": "4",
    "UPSTREAM_TIMEOUT": "10s",
    "LLM_RUNTIME_IMAGE_KEYWORDS": "vllm",
    "ML_RUNTIME_IMAGE_KEYWORDS": "triton, tritonserver",
    "GPU_RESOURCE_NAME": "nvidia.com/gpu",
    "MIG_RESOURCE_PREFIX": "nvidia.com/mig-",
    "CRONWORKFLOW_RESOURCE": "argoproj.io/v1alpha1/cronworkflows",
    "WORKFLOW_RESOURCE": "argoproj.io/v1alpha1/workflows",
    "INFERENCE_SERVICE_RESOURCE": "serving.kserve.io/v1beta1/inferenceservices",
    "SERVING_RUNTIME_RESOURCE": "serving.kserve.io/v1alpha1/servingruntimes",
    "LLM_INFERENCE_SERVICE_RESOURCE": "serving.kserve.io/v1alpha1/llminferenceservices",
    "UI_REFRESH_INTERVAL": "30s",
    "OPENSHIFT_CONSOLE_URL": "https://console.apps.example.test/",
}


class ConfigTest(unittest.TestCase):
    def load(self, **overrides):
        with mock.patch.dict(os.environ, VALID_ENV | overrides, clear=True):
            return config.load()

    def test_parses_config_map_values(self):
        cfg = self.load()
        self.assertEqual((cfg.image_cache_ttl, cfg.ui_refresh_interval, cfg.registry_check_concurrency), (86400, 30, 4))
        self.assertEqual(cfg.serving.ml_image_keywords, ["triton", "tritonserver"])
        self.assertEqual(cfg.resources.llm_inference_services, GVR("serving.kserve.io", "v1alpha1", "llminferenceservices"))
        self.assertEqual(cfg.console_url, "https://console.apps.example.test")

    def test_reports_every_missing_and_invalid_key(self):
        with self.assertRaises(ValueError) as caught:
            self.load(NEXUS_MANIFEST_URL_TEMPLATE="", UI_REFRESH_INTERVAL="", IMAGE_CACHE_TTL="-1h", WORKFLOW_RESOURCE="workflows")
        for key in ("NEXUS_MANIFEST_URL_TEMPLATE", "UI_REFRESH_INTERVAL", "IMAGE_CACHE_TTL", "WORKFLOW_RESOURCE"):
            self.assertIn(key, str(caught.exception))

    def test_requires_manifest_template_placeholders(self):
        with self.assertRaisesRegex(ValueError, r"\{imageId\}"):
            self.load(NEXUS_MANIFEST_URL_TEMPLATE="https://nexus.example.test/{namespace}")

    def test_parse_duration_matches_go(self):
        self.assertEqual(config.parse_duration("1h30m"), 5400)
        self.assertAlmostEqual(config.parse_duration("300ms"), 0.3)
        self.assertEqual(config.parse_duration("-2s"), -2)
        for bad in ("", "5", "5mm", "h"):
            self.assertRaises(ValueError, config.parse_duration, bad)


class BatchTest(unittest.TestCase):
    def test_build_views_maps_latest_workflow_pods_and_schedule(self):
        now = datetime(2026, 9, 27, 11, 15, tzinfo=UTC)
        views = batch.build_views(load("batch/cronworkflows.json"), load("batch/workflows.json"), load("batch/pods.json"), now)
        self.assertEqual(len(views), 1)
        view = views[0]
        self.assertEqual((view["namespace"], view["name"], view["active"]), ("payments-bch", "payout-batch", True))
        self.assertEqual((view["lastRun"]["name"], view["lastRun"]["phase"]), ("payout-latest", "Running"))
        self.assertEqual([(run["name"], run["phase"]) for run in view["history"]], [("payout-latest", "Running"), ("payout-older", "Failed")])
        self.assertEqual(view["lastRun"]["scheduledAt"], datetime(2026, 9, 27, 11, tzinfo=UTC))
        self.assertEqual([(pod["name"], pod["phase"]) for pod in view["lastRun"]["pods"]], [("payout-step", "Running")])
        self.assertEqual(view["nextScheduledAt"], datetime(2026, 9, 27, 11, 30, tzinfo=UTC))
        self.assertEqual(view["images"], [{"reference": "registry.example.test/payments-bch/payout:v3", "imageId": "v3", "status": "unknown"}])

    def test_project_images_reads_every_image_field_with_the_namespace(self):
        spec = {"workflowSpec": {
            "arguments": {"parameters": [{"name": "image", "value": "repomaster:5000/mlops/bch-yazi-girisi-model:not-an-image-field"}]},
            "templates": [
                {"name": "run", "container": {"image": "repomaster:5000/mlops/bch-yazi-girisi-model:ald7383"}},
                {"name": "set", "containerSet": {"containers": [{"name": "a", "image": "repomaster/mlops/bch-yazi-girisi-model@sha256:abc"}]}},
                {"name": "untagged", "script": {"image": "repomaster:5000/mlops/bch-yazi-girisi-model"}},
                {"name": "tool", "container": {"image": "busybox:1.36"}},
            ],
        }}
        # Parameter values are not image fields; an untagged image has no ID; busybox lacks the namespace.
        self.assertEqual([image["imageId"] for image in batch.project_images(spec, "yazi-girisi-model")], ["sha256:abc", "ald7383"])

    def test_schedule_errors_and_every(self):
        now = datetime(2026, 9, 27, 11, 15, 30, 500, tzinfo=UTC)
        self.assertEqual(batch._next_scheduled_time(["@every 1h"], "UTC", now), (datetime(2026, 9, 27, 12, 15, 30, tzinfo=UTC), ""))
        self.assertEqual(batch._next_scheduled_time(["0 * * *"], "UTC", now), (None, "invalid cron expression"))
        self.assertEqual(batch._next_scheduled_time(["0 * * * *"], "Mars/Base", now), (None, "invalid timezone"))
        self.assertEqual(batch._next_scheduled_time(["@daily"], "", now), (datetime(2026, 9, 28, tzinfo=UTC), "timezone unset; next run assumes UTC"))


class ServingTest(unittest.TestCase):
    def models(self, llm=True, pods=False):
        built = serving.build_models(
            load("serving/inferenceservices.json"),
            load("serving/servingruntimes.json"),
            load("serving/llminferenceservices.json") if llm else [],
            load("serving/pods.json") if pods else [],
            RULES,
        )
        return {model["name"]: model for model in built}, built

    def test_classifies_by_runtime_image(self):
        got, built = self.models()
        self.assertEqual(len(built), 5)
        llama = got["llama-chat"]
        self.assertEqual((llama["type"], llama["state"], llama["gpu"], llama["minReplicas"], llama["maxReplicas"]), ("llm", "Ready", 2, 1, 3))
        self.assertEqual(llama["images"], ["quay.io/modh/vllm:rhoai-2.19"])
        self.assertTrue(llama["url"])
        self.assertEqual(llama["storageUri"], "s3://models/llama-3-8b")
        self.assertNotIn("mig", llama)
        fraud = got["fraud-xgb"]
        self.assertEqual((fraud["gpu"], fraud["mig"]), (0, {"1g.5gb": 2, "3g.20gb": 1}))
        self.assertEqual((fraud["type"], fraud["state"], fraud["reason"]), ("ml", "NotReady", "RevisionMissing"))
        self.assertIsNotNone(fraud.get("stateSince"))
        granite = got["granite-rag"]
        self.assertEqual((granite["type"], granite["kind"], granite["gpu"], granite["minReplicas"], granite["state"]), ("llm", "LLMInferenceService", 1, 2, "NotReady"))

    def test_classify_prefers_llm_keywords(self):
        self.assertEqual(serving.classify(["registry/vllm-triton-bridge:1"], RULES), "llm")

    def test_other_runtimes_are_custom_serve_with_pods(self):
        got, _ = self.models(llm=False, pods=True)
        self.assertTrue(got["dangling-runtime"]["runtimeMissing"])
        self.assertNotIn("runtimeMissing", got["openvino-model"])
        # The InferenceService sets no GPU, so the ServingRuntime container's limit counts.
        self.assertEqual(got["openvino-model"]["gpu"], 1)
        for name in ("openvino-model", "dangling-runtime"):
            self.assertEqual(got[name]["type"], "custom")
        pods = got["fraud-xgb"]["pods"]
        self.assertEqual([(p["name"], p["ready"], p["containers"], p["restarts"]) for p in pods], [("fraud-xgb-predictor-abc", 1, 2, 7)])
        self.assertEqual(pods[0]["containerStates"], ["kserve-container: Waiting (CrashLoopBackOff)", "queue-proxy: Running"])

    def test_quantity_value(self):
        cases = [("2", 2), (1, 1), ("500m", 1), ("1Ki", 1024), ("1k", 1000), ("1e3", 1000), ("1E", 10**18), ("1.5", 2), ("abc", None), (True, None)]
        for value, want in cases:
            self.assertEqual(serving.quantity_value(value), want, value)


def obj(api_version, kind, ns, name, spec):
    return {"apiVersion": api_version, "kind": kind, "metadata": {"name": name, "namespace": ns}, "spec": spec}


class FakeImages:
    def __init__(self, test):
        self.test = test

    def manifest_url(self, ns, image_id):
        return f"https://nexus.example.test/bch-{ns}/manifests/{image_id}"

    def lookup(self, urls):
        want = "https://nexus.example.test/bch-payments/manifests/v1"
        self.test.assertEqual(urls, [want])
        return {want: {"url": want, "status": "exist", "checkedAt": datetime(2026, 9, 27, 12, 1, tzinfo=UTC)}}


def cron_with_image(ns, image):
    return obj("argoproj.io/v1alpha1", "CronWorkflow", ns, "daily-job", {
        "schedule": "0 12 * * *",
        "workflowSpec": {"templates": [{"name": "run", "container": {"image": image}}]},
    })


class DashboardTest(unittest.TestCase):
    def board(self):
        runtime = obj("serving.kserve.io/v1alpha1", "ServingRuntime", "chatbot", "vllm", {"containers": [{"image": "quay.io/vllm/vllm-openai:0.6"}]})
        isvc = obj("serving.kserve.io/v1beta1", "InferenceService", "chatbot", "llama", {"predictor": {"model": {"runtime": "vllm"}}})
        objects = {
            RESOURCES.cron_workflows: [
                cron_with_image("payments", "registry.example.test/mlops/bch-payments:v1"),
                cron_with_image("other-team", "busybox:1.36"),
                cron_with_image("cm-proje", "registry.example.test/cm/app:v2"),
            ],
            RESOURCES.serving_runtimes: [runtime],
            RESOURCES.inference_services: [isvc],
            NAMESPACES: [{"metadata": {"name": n}} for n in ("payments", "chatbot", "other-team", "cm-proje")],
        }
        return dashboard.Dashboard(dashboard.Sources(
            projects=lambda: projects.Snapshot(projects=[
                {"key": "PAYMENTS", "namespace": "payments"},
                {"key": "GHOST", "namespace": "ghost"},
                {"key": "CM_PROJE", "namespace": "cm-proje"},
            ]),
            cluster=lambda: Snapshot(objects, {"state": "ready"}),
            pods=lambda: Snapshot({}, {"state": "ready"}),
            images=FakeImages(self),
        ), make_config(console_url="https://console.example.test"))

    def test_endpoint_combines_batch_and_models(self):
        with serve(self.board(), tempfile.mkdtemp()) as base:
            request = urllib.request.Request(base + "/api/dashboard", headers={"Accept-Encoding": "gzip"})
            with urllib.request.urlopen(request) as resp:
                self.assertEqual(resp.headers["Content-Encoding"], "gzip")
                response = json.loads(gzip.decompress(resp.read()))
            with self.assertRaises(urllib.error.HTTPError) as caught:
                urllib.request.urlopen(urllib.request.Request(base + "/api/dashboard", method="POST"))
            caught.exception.close()
            self.assertEqual((caught.exception.code, caught.exception.headers["Allow"]), (405, "GET"))

        self.assertEqual(response["console"]["refs"]["InferenceService"], "serving.kserve.io~v1beta1~InferenceService")
        self.assertEqual(response["console"]["url"], "https://console.example.test")
        self.assertEqual(response["refreshIntervalSeconds"], 30)
        # Every CronWorkflow in the cluster is shown; images with the namespace are checked.
        self.assertEqual([f"{row['namespace']}:{len(row['images'])}" for row in response["cronWorkflows"]], ["cm-proje:0", "other-team:0", "payments:1"])
        self.assertEqual(response["cronWorkflows"][2]["images"][0]["status"], "exist")
        self.assertEqual(response["sources"]["registry"], {"state": "ready", "lastSuccess": "2026-09-27T12:01:00Z"})
        self.assertEqual([(m["type"], m["name"]) for m in response["models"]], [("llm", "llama")])
        self.assertEqual(response["namespaces"], ["chatbot", "cm-proje", "ghost", "other-team", "payments"])
        # Coverage shows the project without a namespace instead of hiding it.
        self.assertEqual(response["projects"], [
            {"key": "PAYMENTS", "namespace": "payments", "namespaceExists": True, "cronWorkflows": 1, "inferenceServices": 0, "pods": 0},
            {"key": "GHOST", "namespace": "ghost", "namespaceExists": False, "cronWorkflows": 0, "inferenceServices": 0, "pods": 0},
            {"key": "CM_PROJE", "namespace": "cm-proje", "namespaceExists": True, "cronWorkflows": 1, "inferenceServices": 0, "pods": 0},
        ])

    def test_serves_precompressed_assets_and_spa_fallback(self):
        web_dir = tempfile.mkdtemp()
        os.makedirs(os.path.join(web_dir, "assets"))
        for name, content in {"index.html": "<html></html>", "assets/app-123.js": "plain", "assets/app-123.js.gz": "zipped"}.items():
            with open(os.path.join(web_dir, name), "w") as file:
                file.write(content)

        def get(base, path, encoding):
            with urllib.request.urlopen(urllib.request.Request(base + path, headers={"Accept-Encoding": encoding})) as resp:
                return resp.read().decode(), resp.headers

        with serve(None, web_dir) as base:
            body, headers = get(base, "/assets/app-123.js", "gzip, br")
            self.assertEqual((body, headers["Content-Encoding"], headers["Cache-Control"]), ("zipped", "gzip", "public, max-age=31536000, immutable"))
            self.assertEqual(get(base, "/assets/app-123.js", "")[0], "plain")
            body, headers = get(base, "/batch", "gzip")
            self.assertEqual((body, headers["Cache-Control"]), ("<html></html>", "no-cache"))
            self.assertEqual(get(base, "/../../etc/passwd", "")[0], "<html></html>")
            self.assertEqual(get(base, "/healthz", "")[0], "ok\n")


@contextmanager
def serve(board, web_dir):
    server = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(board, web_dir))
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        yield f"http://127.0.0.1:{server.server_port}"
    finally:
        server.shutdown()
        server.server_close()


class FakeResponse(io.BytesIO):
    def __init__(self, status, body=b""):
        super().__init__(body)
        self.status = status


TEMPLATE = "https://nexus.example.test/repository/company-private/v2/mlops/bch-{namespace}/manifests/{imageId}"


class RegistryTest(unittest.TestCase):
    def test_manifest_url_fills_template(self):
        checker = registry.Checker(TEMPLATE, 3600, 1, 1)
        self.assertEqual(checker.manifest_url("yazi-girisi-model", "ald7383"), "https://nexus.example.test/repository/company-private/v2/mlops/bch-yazi-girisi-model/manifests/ald7383")

    def test_lookup_checks_in_background_and_reuses_cache(self):
        requests = []

        def opener(request, timeout):
            requests.append(request.full_url)
            if request.full_url.endswith("/manifests/exists"):
                return FakeResponse(200)
            raise urllib.error.HTTPError(request.full_url, 404, "Not Found", {}, io.BytesIO())

        checker = registry.Checker(TEMPLATE, 86400, 1, 1, opener=opener)
        exists, absent = checker.manifest_url("payments", "exists"), checker.manifest_url("payments", "absent")
        with self.assertLogs("monitor.registry", "INFO") as logs:
            first = checker.lookup([exists, absent, exists])
            self.assertEqual((first[exists]["status"], first[absent]["status"]), ("checking", "checking"))
            deadline = time.monotonic() + 2
            while len(checker.cache) < 2 and time.monotonic() < deadline:
                time.sleep(0.005)
            self.assertGreater(checker.cache[absent][1] - time.monotonic(), 23 * 3600)
            second = checker.lookup([exists, absent])
        self.assertEqual((second[exists]["status"], second[absent]["status"], len(requests)), ("exist", "missing", 2))
        output = "\n".join(logs.output)
        for want in (
            'msg="registry image check cycle" references=2 started=2',
            f'msg="registry image check request" method=GET url={absent}',
            f'msg="registry image check result" url={absent} status=404 state=missing',
            'msg="registry image check cycle" references=2 started=0 cache_hits=2',
        ):
            self.assertIn(want, output)


class ProjectsTest(unittest.TestCase):
    def test_refresh_turns_every_key_into_a_namespace(self):
        with open(os.path.join(TESTDATA, "projects/projects.json"), "rb") as file:
            data = file.read()
        seen = []

        def opener(request, timeout):
            seen.append(request.full_url)
            return FakeResponse(200, data)

        source = projects.Source(make_config(azure_repo_url="https://dev.azure.com/org/proj/_git/repo"), opener=opener)
        source.refresh()
        snapshot = source.snapshot()
        self.assertFalse(snapshot.stale, snapshot.error)
        self.assertEqual(snapshot.namespaces(), ["payments-api", "retail-model", "platform-tools", "unclassified"])
        self.assertTrue(seen[0].startswith("https://dev.azure.com/org/proj/_apis/git/repositories/repo/items?"))
        self.assertIn("path=%2Fprojects.json", seen[0])

    def test_parse_projects_skips_duplicate_namespaces_instead_of_failing(self):
        found, skipped = projects.parse_projects({"YAZI_GIRISI_MODEL": {"Serving": ["BCH"]}, "ANY_VALUE": ["not", "an", "object"], "dup-proje": {}, "DUP_PROJE": {}})
        self.assertEqual(projects.Snapshot(projects=found).namespaces(), ["any-value", "dup-proje", "yazi-girisi-model"])
        self.assertEqual(len(skipped), 1)


class FakeAPI:
    """Serves a scripted list, then watch streams, for one resource."""

    def __init__(self, resources, pages, streams):
        self.resources, self.pages, self.streams = resources, pages, streams

    def get(self, path):
        if "?" not in path:  # discovery
            if path not in self.resources:
                raise kube.APIError(404, "not found")
            return {"resources": [{"name": n} for n in self.resources[path]]}
        return self.pages.pop(0)

    @contextmanager
    def stream(self, path, timeout):
        if not self.streams:
            threading.Event().wait()  # an idle watch
        yield None, [json.dumps(event).encode() + b"\n" for event in self.streams.pop(0)]


def pod(name, version):
    return {"metadata": {"name": name, "namespace": "ns", "resourceVersion": version, "managedFields": [{}]}}


class KubeTest(unittest.TestCase):
    def test_served_resources_skips_missing_crds(self):
        api = FakeAPI({"/apis/serving.kserve.io/v1beta1": ["inferenceservices"], "/apis/serving.kserve.io/v1alpha1": ["servingruntimes"]}, [], [])
        isvc, runtimes = GVR("serving.kserve.io", "v1beta1", "inferenceservices"), GVR("serving.kserve.io", "v1alpha1", "servingruntimes")
        llmisvc, cron = GVR("serving.kserve.io", "v1alpha1", "llminferenceservices"), GVR("argoproj.io", "v1alpha1", "cronworkflows")
        served, missing = kube.served_resources(api, [isvc, runtimes, llmisvc, cron])
        self.assertEqual(served, [isvc, runtimes])
        self.assertEqual(missing, ["serving.kserve.io/v1alpha1/llminferenceservices", "argoproj.io/v1alpha1/cronworkflows"])

    def test_informer_lists_applies_watch_events_and_relists_on_410(self):
        api = FakeAPI({"/api/v1": ["pods"]}, [
            {"kind": "PodList", "apiVersion": "v1", "metadata": {"resourceVersion": "1", "continue": "next"}, "items": [pod("a", "1")]},
            {"kind": "PodList", "apiVersion": "v1", "metadata": {"resourceVersion": "1"}, "items": [pod("b", "1")]},
            {"kind": "PodList", "apiVersion": "v1", "metadata": {"resourceVersion": "9"}, "items": [pod("c", "9"), pod("d", "9")]},
        ], [[
            {"type": "ADDED", "object": pod("e", "2")},
            {"type": "DELETED", "object": pod("a", "3")},
            {"type": "BOOKMARK", "object": {"metadata": {"resourceVersion": "4"}}},
        ], [
            {"type": "ERROR", "object": {"code": 410, "message": "too old"}},
        ]])
        watcher = kube.Watcher("test", api, [kube.PODS], kube.ALL_NAMESPACES)
        watcher.reconcile()
        deadline = time.monotonic() + 2
        while api.pages and time.monotonic() < deadline:
            time.sleep(0.005)
        time.sleep(0.05)
        snapshot = watcher.snapshot()
        watcher.reconcile()
        items = sorted(snapshot.objects[kube.PODS], key=kube.name)
        # The relist after the 410 replaced the first list and its watch events.
        self.assertEqual([kube.name(item) for item in items], ["c", "d"])
        self.assertEqual((items[0]["kind"], items[0]["apiVersion"]), ("Pod", "v1"))
        self.assertNotIn("managedFields", items[0]["metadata"])
        self.assertEqual(snapshot.health["state"], "ready")


if __name__ == "__main__":
    unittest.main()
