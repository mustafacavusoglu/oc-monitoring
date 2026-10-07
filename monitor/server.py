"""Entry point: starts the watchers and serves the API, a health probe and the
single-page app. Run with `python -m monitor.server` on a free-threaded
(no-GIL) interpreter so request threads and watch threads run in parallel."""

import logging
import mimetypes
import os
import posixpath
import signal
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import unquote, urlsplit

from . import config, dashboard, kube, projects, registry

log = logging.getLogger("monitor")


def make_handler(api: dashboard.Dashboard, web_dir: str):
    """Hashed build assets are cached forever and served from their build-time
    .gz sibling when the client accepts gzip; index.html is always revalidated."""

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"  # keep-alive
        timeout = 120  # closes idle keep-alive connections

        def log_message(self, *args):  # requests are not logged
            pass

        def do_GET(self):
            self._route(head=False)

        def do_HEAD(self):
            self._route(head=True)

        def do_POST(self):
            self._route(head=False)

        do_PUT = do_DELETE = do_PATCH = do_OPTIONS = do_POST

        def _route(self, head: bool):
            path = urlsplit(self.path).path
            if path == "/healthz":
                self._send(200, b"ok\n", {"Content-Type": "text/plain; charset=utf-8"}, head)
            elif path.startswith("/api/"):
                self._api(path)
            elif self.command not in ("GET", "HEAD"):
                self._send(405, b"method not allowed\n", {"Content-Type": "text/plain; charset=utf-8"})
            else:
                self._static(path, head)

        def _api(self, path: str):
            if path != "/api/dashboard":
                self._send(404, b"404 page not found\n", {"Content-Type": "text/plain; charset=utf-8"})
                return
            if self.command != "GET":
                self._send(405, b"method not allowed\n", {"Content-Type": "text/plain; charset=utf-8", "Allow": "GET"})
                return
            body, gzipped = api.response()
            headers = {"Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store", "Vary": "Accept-Encoding"}
            if "gzip" in self.headers.get("Accept-Encoding", ""):
                body, headers["Content-Encoding"] = gzipped, "gzip"
            self._send(200, body, headers)

        def _static(self, path: str, head: bool):
            name = posixpath.normpath(unquote(path).lstrip("/"))
            if name in (".", "") or name.startswith("..") or "\0" in name or not os.path.isfile(os.path.join(web_dir, name)):
                name = "index.html"
            file = os.path.join(web_dir, name)
            content_type = mimetypes.guess_type(name)[0] or "application/octet-stream"
            if content_type.startswith("text/"):
                content_type += "; charset=utf-8"
            headers = {
                "Content-Type": content_type,
                "Cache-Control": "public, max-age=31536000, immutable" if name.startswith("assets/") else "no-cache",
                "Vary": "Accept-Encoding",
            }
            if "gzip" in self.headers.get("Accept-Encoding", "") and os.path.isfile(file + ".gz"):
                file, headers["Content-Encoding"] = file + ".gz", "gzip"
            try:
                with open(file, "rb") as handle:
                    body = handle.read()
            except OSError:
                self._send(404, b"404 page not found\n", {"Content-Type": "text/plain; charset=utf-8"})
                return
            self._send(200, body, headers, head)

        def _send(self, status: int, body: bytes, headers: dict, head: bool = False):
            self.send_response(status)
            for header, value in headers.items():
                self.send_header(header, value)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            if not head:
                self.wfile.write(body)

    return Handler


class Server(ThreadingHTTPServer):
    request_queue_size = 128


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(message)s", datefmt="%Y/%m/%d %H:%M:%S")
    try:
        cfg = config.load()
        api = kube.API()
    except (ValueError, RuntimeError) as error:
        log.error("%s", error)
        sys.exit(1)

    res = cfg.resources
    project_source = projects.Source(cfg)
    # Everything is watched cluster-wide with one informer per resource; only
    # pods are watched per namespace, where CronWorkflows (last-run pods) and
    # InferenceServices (serving pods) run.
    cluster_watcher = kube.Watcher("cluster", api, [res.cron_workflows, res.workflows, res.inference_services, res.serving_runtimes, res.llm_inference_services, kube.NAMESPACES], kube.ALL_NAMESPACES)

    def pod_namespaces() -> list[str]:
        objects = cluster_watcher.snapshot().objects
        return [kube.namespace(obj) for gvr in (res.cron_workflows, res.inference_services, res.llm_inference_services) for obj in objects.get(gvr, [])]

    pod_watcher = kube.Watcher("pods", api, [kube.PODS], pod_namespaces)
    image_checker = registry.Checker(cfg.nexus_manifest_url_template, cfg.image_cache_ttl, cfg.registry_check_concurrency, cfg.upstream_timeout)

    stop = threading.Event()
    for run in (project_source.run, cluster_watcher.run, pod_watcher.run):
        threading.Thread(target=run, args=(stop,), daemon=True).start()

    board = dashboard.Dashboard(dashboard.Sources(
        projects=project_source.snapshot,
        cluster=cluster_watcher.snapshot,
        pods=pod_watcher.snapshot,
        images=image_checker,
    ), cfg)
    host, _, port = cfg.http_addr.rpartition(":")
    server = Server((host, int(port)), make_handler(board, cfg.web_dir))

    def shutdown(*_):
        stop.set()
        threading.Thread(target=server.shutdown).start()

    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)
    gil = sys._is_gil_enabled() if hasattr(sys, "_is_gil_enabled") else True
    log.info("dashboard listening on %s (Python %s, GIL %s)", cfg.http_addr, sys.version.split()[0], "enabled" if gil else "disabled")
    server.serve_forever()


if __name__ == "__main__":
    main()
