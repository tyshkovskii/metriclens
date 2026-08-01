# metriclens example: Python client library

This project shows application-owned instrumentation with the official
[`prometheus_client`](https://github.com/prometheus/client_python) package.
The service updates its counters, histogram, and queue gauge in Python and
serves the registry at `/metrics`. The client also exports its built-in
`process_*` and `python_*` runtime metrics.

| Service | Metrics endpoint | What it exposes |
| --- | --- | --- |
| `app` | `http://app:8000/metrics` | `python_http_requests_total` counter, `python_request_duration_seconds` histogram, `python_queue_depth` gauge, and client runtime metrics |

## Run

```bash
# from this directory
docker compose up --build
```

Then open <http://localhost:9999>. The `app` target should be **UP**, and its
counter and queue values should change every few seconds.

## Tear down

```bash
docker compose down
```
