# metriclens

metriclens is a zero-config Prometheus metrics explorer for Docker Compose development environments and the coding agents working in them.

Run it as one service in your Compose project. It discovers sibling containers, probes common metrics endpoints, scrapes Prometheus-format metrics, and exposes a local UI on port `9999` with raw metrics, generated charts, and instrumentation quality warnings.

## Quick start

Add metriclens to your existing `docker-compose.yml`:

```yaml
services:
  metriclens:
    image: tyshkovskii/metriclens:latest
    ports:
      - "127.0.0.1:9999:9999"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

Then start your project:

```bash
docker compose up -d
```

Open <http://localhost:9999>.

For repeatable environments, pin an exact version from the [tags page](https://hub.docker.com/r/tyshkovskii/metriclens/tags).

## For coding agents

The same port serves agent interfaces with bounded responses:

- `http://localhost:9999/mcp`: Streamable HTTP MCP server with `wait_for_stack`, `start_experiment`, `compare_experiment`, `get_metric_evidence`, `list_metrics`, and `observe_stack`.
- `http://localhost:9999/llms.txt`: a short usage guide. `/openapi.json` and `/api/capabilities` describe the HTTP API and its limits.
- `metriclensctl`: a compact JSON CLI in the image, for example `docker compose exec metriclens metriclensctl wait --services api --timeout 60s`.

Findings link to the matching service, metric, and time window in the UI, so a person can check what the agent reports.

## Configuration

Usually none is needed. If metriclens can't find a service's metrics endpoint, point it at the right port or path with labels:

```yaml
services:
  api:
    labels:
      metriclens.port: "8080"
      metriclens.path: "/metrics"
```

To hide a service from metriclens:

```yaml
labels:
  metriclens.exclude: "true"
```

Container environment variables:

- `metriclens_SCRAPE_INTERVAL`: scrape interval, default `5s`
- `metriclens_RETENTION`: in-memory retention window, default `15m`

## Tags and platforms

Release tags are published as:

- `latest`: latest published release
- `<major>.<minor>`: latest patch in a minor release line, for example `0.2`
- `<major>.<minor>.<patch>`: exact release, for example `1.1.0`

Images are published for `linux/amd64` and `linux/arm64`.

## Security note

metriclens is intended for local Docker Compose development. Mounting the Docker socket can expose broad access to the local Docker daemon, so only use it with trusted local tooling.

## Links

- GitHub: https://github.com/tyshkovskii/metriclens
- Issues: https://github.com/tyshkovskii/metriclens/issues
- License: MIT
