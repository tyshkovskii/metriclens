![metriclens UI](docs/screenshot.png)

# metriclens [![Docker image](https://img.shields.io/docker/v/tyshkovskii/metriclens?sort=semver&logo=docker&label=docker)](https://hub.docker.com/r/tyshkovskii/metriclens/tags) [![Docker pulls](https://img.shields.io/docker/pulls/tyshkovskii/metriclens?logo=docker)](https://hub.docker.com/r/tyshkovskii/metriclens)

metriclens is a zero-config, drop-in observability layer for Compose-based development environments, automatically discovering Prometheus metrics and surfacing live charts and instrumentation issues without requiring Prometheus or Grafana.

## Try it

The [basic example](example/basic) runs metriclens alongside two instrumented services generating live traffic:

```bash
git clone https://github.com/tyshkovskii/metriclens.git
cd metriclens/example/basic
docker compose up --build
```

Open <http://localhost:9999>. You'll see both services discovered and scraped, with:

- **Raw metrics** — every metric with its help text, type, and labels, updated live.
- **Panels** — charts built automatically from metric types: rates for counters, current values for gauges, latency percentiles for histograms.
- **Quality warnings** — missing `HELP` or `TYPE`, counters not named `*_total`, labels that look high-cardinality.

See the [example index](example) for additional projects showing application
client-library metrics, an exporter for an uninstrumented Redis service, and a
Pushgateway-backed batch job.

## Use it in your project

Published Docker images are available on [Docker Hub](https://hub.docker.com/r/tyshkovskii/metriclens).

Add one service to your existing `docker-compose.yml`:

```yaml
services:
  metriclens:
    image: tyshkovskii/metriclens:latest
    ports:
      - "127.0.0.1:9999:9999"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

metriclens finds the other services in your Compose project on its own, locates their metrics endpoints (it tries common ports and paths like `/metrics`), and starts scraping.

Pin a version from the [Docker Hub tags page](https://hub.docker.com/r/tyshkovskii/metriclens/tags) if you want repeatable environments.

## Agent and tool discovery

Four small discovery surfaces make MetricLens easy to use from development agents:

- [`/openapi.json`](http://localhost:9999/openapi.json) is the complete static OpenAPI contract.
- [`/llms.txt`](http://localhost:9999/llms.txt) is concise agent guidance and workflow context.
- [`/mcp`](http://localhost:9999/mcp) is the Streamable HTTP MCP endpoint with five bounded tools: `observe_stack`, `wait_for_stack`, `start_experiment`, `compare_experiment`, and `get_metric_evidence`.
- [`/api/capabilities`](http://localhost:9999/api/capabilities) is machine-readable runtime truth, including the active timing configuration, implemented features, limits, and links to the other discovery surfaces.

Use MCP for the wait → marker → workload → compare → evidence workflow, `llms.txt` for instructions, and `api/capabilities` for values that may differ between environments.
The `/llm` path is intentionally absent; `/llms.txt` is the standard discovery document.

## Configuration

Usually none is needed. If metriclens can't find a service's metrics endpoint, point it at the right port with a label:

```yaml
services:
  api:
    labels:
      metriclens.port: "8080"
      metriclens.path: "/metrics"
```

To hide a service from metriclens, label it `metriclens.exclude: "true"`.

On the metriclens container itself you can tune two environment variables: `metriclens_SCRAPE_INTERVAL` (default `5s`) and `metriclens_RETENTION` (default `15m`, metrics are kept in memory only).

## License

[MIT](LICENSE)
