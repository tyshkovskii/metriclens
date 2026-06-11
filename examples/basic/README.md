# metriclens example: basic

A minimal Docker Compose project that runs **metriclens** alongside two example
services so you can see the whole flow end to end.

| Service | Metrics endpoint | What it exposes |
| --- | --- | --- |
| `api` | `http://api:8080/metrics` | `http_requests_total` (counter, `method`/`route`/`status`), `http_request_duration_seconds` histogram, `process_resident_memory_bytes` gauge |
| `worker` | `http://worker:9090/metrics` | `jobs_processed_total` (counter), `worker_queue_depth` gauge, `process_resident_memory_bytes` gauge |

Both services simulate live traffic, so counters grow and the generated rate
panels actually move.

## Run

```bash
# from this directory
docker compose up --build
```

Then open <http://localhost:9999>.

## What you should see

- Both `api` and `worker` listed as **UP** targets.
- **Raw Metrics** tab: all metrics above with their HELP text, type, and labels.
- **Generated Panels** tab: HTTP request rate and latency (p95) for `api`,
  counter-rate and gauge panels for `worker`.
- **Quality** tab: actionable warnings (e.g. metrics missing HELP/TYPE, or
  high-cardinality label hints).

## Tear down

```bash
docker compose down
```
