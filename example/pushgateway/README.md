# metriclens example: Pushgateway batch job

This project shows job-owned instrumentation. A one-shot `batch` container
pushes its final counters and gauges to
[`Pushgateway`](https://github.com/prometheus/pushgateway), which remains up
for metriclens to scrape. The batch container is excluded because it exits
after the push; the grouping path adds `job="nightly-report"` and
`instance="batch"` labels to the pushed series.

| Service | Metrics endpoint | What it exposes |
| --- | --- | --- |
| `pushgateway` | `http://pushgateway:9091/metrics` | Pushed `batch_items_processed_total`, `batch_job_last_success_unixtime`, `batch_job_duration_seconds`, plus Pushgateway runtime metrics |
| `batch` | — | One-shot job that pushes metrics, then exits and is excluded from discovery |

## Run

```bash
# from this directory
docker compose up --build
```

Then open <http://localhost:9999>. The `pushgateway` target should be **UP**
and show the batch metrics with their grouping labels. Run the job again to
replace the values in Pushgateway:

```bash
docker compose run --rm batch
```

## Tear down

```bash
docker compose down
```
