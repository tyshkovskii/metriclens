# metriclens examples

Each directory is a self-contained Docker Compose project. Run one at a time
from its directory; every project builds the local metriclens image and opens
the UI on <http://localhost:9999>.

| Example | Instrumentation ownership | Start here |
| --- | --- | --- |
| [basic](basic) | Go services emit Prometheus text directly | Two services with counters, gauges, and a histogram |
| [python-client](python-client) | The application owns metrics through `prometheus_client` | Client-library counters, histogram, gauges, and runtime metrics |
| [redis-exporter](redis-exporter) | An exporter translates an uninstrumented service | Redis plus `redis_exporter` and a small load generator |
| [pushgateway](pushgateway) | A batch job pushes its final values | Pushgateway grouping labels and one-shot job metrics |

All examples use the same optional Compose labels when discovery needs help:
`metriclens.port` and `metriclens.path` select a metrics endpoint, while
`metriclens.exclude: "true"` hides services that do not expose Prometheus
metrics (such as Redis itself or a one-shot batch job).
