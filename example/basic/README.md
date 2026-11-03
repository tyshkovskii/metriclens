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

## Seed a regression

The `api` service accepts two fault knobs so you can check that a comparison
catches a known regression: `ERROR_RATE` (share of 500 responses, default
`0.07`) and `LATENCY_MS` (added to every request, default `0`).

```bash
docker compose up -d --build
# Let normal traffic run for a minute, then mark the change.
curl -s -X POST http://localhost:9999/api/markers -d '{"name":"seeded errors"}'
ERROR_RATE=0.3 docker compose up -d api   # or LATENCY_MS=300
# After another minute, compare with the equal-length window before the marker.
curl -s "http://localhost:9999/api/compare?from=MARKER_ID&severity=warning,error"
```

Expect an `error_ratio` finding (or `latency_p95` for `LATENCY_MS`) with
`"verdict": "regressed"`. Run `docker compose up -d api` without the knobs to
restore normal traffic; a comparison across unchanged traffic reports
`unchanged` verdicts, which the `warning,error` filter hides.

## Tear down

```bash
docker compose down
```
