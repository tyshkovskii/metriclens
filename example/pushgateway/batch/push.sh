#!/bin/sh
set -eu

until curl --fail --silent --show-error --max-time 2 http://pushgateway:9091/-/ready >/dev/null; do
  sleep 1
done

timestamp=$(date +%s)
items=$((timestamp % 100 + 100))
duration=$((timestamp % 7 + 1))

cat <<METRICS | curl --fail --silent --show-error \
  -H 'Content-Type: text/plain; version=0.0.4' \
  --data-binary @- \
  http://pushgateway:9091/metrics/job/nightly-report/instance/batch
# HELP batch_items_processed_total Items processed by the scheduled batch job.
# TYPE batch_items_processed_total counter
batch_items_processed_total ${items}
# HELP batch_job_last_success_unixtime Unix time of the last successful batch run.
# TYPE batch_job_last_success_unixtime gauge
batch_job_last_success_unixtime ${timestamp}
# HELP batch_job_duration_seconds Duration of the scheduled batch job in seconds.
# TYPE batch_job_duration_seconds gauge
batch_job_duration_seconds ${duration}
METRICS

echo "pushed nightly-report metrics for ${timestamp}"
