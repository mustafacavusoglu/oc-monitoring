"""Checks whether image manifests exist in Nexus. Lookups never block a
dashboard request: results are served from a TTL cache and expired or unknown
URLs are refreshed in the background."""

import logging
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from datetime import UTC, datetime
from urllib.parse import quote

log = logging.getLogger(__name__)

MANIFEST_ACCEPT = (
    "application/vnd.docker.distribution.manifest.v2+json, "
    "application/vnd.docker.distribution.manifest.list.v2+json, "
    "application/vnd.oci.image.manifest.v1+json, "
    "application/vnd.oci.image.index.v1+json, "
    "application/json"
)


class Checker:
    def __init__(self, url_template: str, ttl: float, max_concurrent: int, timeout: float, opener=urllib.request.urlopen):
        self.url_template, self.ttl, self.timeout, self.opener = url_template, ttl, timeout, opener
        self._pool = ThreadPoolExecutor(max_concurrent, thread_name_prefix="registry")
        self._lock = threading.Lock()
        self.cache: dict[str, tuple[dict, float]] = {}  # url -> (result, expires at, monotonic)
        self._inflight: set[str] = set()

    def manifest_url(self, ns: str, image_id: str) -> str:
        """Fills the configured template's {namespace} and {imageId}."""
        return self.url_template.replace("{namespace}", quote(ns, safe="")).replace("{imageId}", quote(image_id, safe=""))

    def lookup(self, urls: list[str]) -> dict[str, dict]:
        """The cached result for each URL, or "checking" while a background refresh is running."""
        results: dict[str, dict] = {}
        started = cache_hits = in_flight = 0
        now = time.monotonic()
        with self._lock:
            for url in urls:
                if url in results:
                    continue
                cached = self.cache.get(url)
                if cached and now < cached[1]:
                    cache_hits += 1
                elif url in self._inflight:
                    in_flight += 1
                else:
                    started += 1
                    self._inflight.add(url)
                    self._pool.submit(self._refresh, url)
                results[url] = cached[0] if cached else {"reference": url, "url": url, "status": "checking"}
        log.info('msg="registry image check cycle" references=%d started=%d cache_hits=%d in_flight=%d', len(results), started, cache_hits, in_flight)
        return results

    def _refresh(self, url: str):
        result = self._get(url)
        with self._lock:
            self.cache[url] = (result, time.monotonic() + self.ttl)
            self._inflight.discard(url)

    def _get(self, url: str) -> dict:
        result = {"reference": url, "url": url, "checkedAt": datetime.now(UTC), "status": "error"}
        log.info('msg="registry image check request" method=GET url=%s', url)
        request = urllib.request.Request(url, headers={"Accept": MANIFEST_ACCEPT})
        try:
            with self.opener(request, timeout=self.timeout) as resp:
                resp.read(1 << 20)
                status = resp.status
        except urllib.error.HTTPError as error:
            status = error.code
            error.close()
        except Exception as error:  # noqa: BLE001 - reported as the image's error
            result["error"] = str(error)
            log.info('msg="registry image check failed" method=GET url=%s error="%s"', url, error)
            return result
        if status == 200:
            result["status"] = "exist"
        elif status == 404:
            result["status"] = "missing"
        else:
            result["error"] = f"unexpected HTTP status {status}"
        log.info('msg="registry image check result" url=%s status=%d state=%s', url, status, result["status"])
        return result
