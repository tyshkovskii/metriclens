"""Small HTTP service instrumented with the official Prometheus Python client."""

from __future__ import annotations

import os
import random
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

from prometheus_client import CONTENT_TYPE_LATEST, Counter, Gauge, Histogram, generate_latest


REQUESTS = Counter(
    "python_http_requests_total",
    "HTTP requests handled by the example Python service.",
    ("method", "route", "status"),
)
LATENCY = Histogram(
    "python_request_duration_seconds",
    "HTTP request latency in seconds.",
    ("route",),
)
QUEUE_DEPTH = Gauge("python_queue_depth", "Current number of queued work items.")


class Handler(BaseHTTPRequestHandler):
    """Serve a tiny application endpoint and the Prometheus exposition."""

    def do_GET(self) -> None:  # noqa: N802 - method name is defined by BaseHTTPRequestHandler.
        path = urlparse(self.path).path
        if path == "/metrics":
            payload = generate_latest()
            self.send_response(200)
            self.send_header("Content-Type", CONTENT_TYPE_LATEST)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return

        started = time.perf_counter()
        status = 200 if path in {"/", "/health"} else 404
        if status == 200:
            body = b"python client example\n"
        else:
            body = b"not found\n"

        REQUESTS.labels(method="GET", route=path, status=str(status)).inc()
        LATENCY.labels(route=path).observe(time.perf_counter() - started)
        self.send_response(status)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, _format: str, *_args: object) -> None:
        # Keep the example output focused on the metrics endpoint.
        return


def simulate_work() -> None:
    """Generate changing series so the metriclens charts are useful immediately."""

    rng = random.Random()
    routes = ("/", "/health", "/orders", "/users")
    while True:
        for _ in range(3 + rng.randrange(12)):
            route = rng.choice(routes)
            status = "500" if rng.random() < 0.05 else "200"
            REQUESTS.labels(method="GET", route=route, status=status).inc()
            LATENCY.labels(route=route).observe(rng.expovariate(8))
        QUEUE_DEPTH.set(rng.randrange(40))
        time.sleep(1)


def main() -> None:
    port = int(os.environ.get("PORT", "8000"))
    threading.Thread(target=simulate_work, daemon=True).start()
    server = ThreadingHTTPServer(("", port), Handler)
    print(f"python client example listening on :{port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
