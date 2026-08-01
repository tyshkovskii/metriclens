# metriclens example: Redis exporter

This project shows exporter-owned instrumentation. Redis is an otherwise
uninstrumented service; [`redis_exporter`](https://github.com/oliver006/redis_exporter)
connects to it and translates Redis `INFO` data into Prometheus metrics. A
small helper container writes keys once per second so the exported counters
change while you explore them.

| Service | Metrics endpoint | What it exposes |
| --- | --- | --- |
| `exporter` | `http://exporter:9121/metrics` | `redis_up`, `redis_connected_clients`, `redis_commands_processed_total`, `redis_memory_used_bytes`, and other Redis INFO metrics |
| `redis` | — | Excluded from discovery because it has no Prometheus endpoint |
| `traffic` | — | Excluded load generator that uses `redis-cli` |

## Run

```bash
# from this directory
docker compose up --build
```

Then open <http://localhost:9999>. The `exporter` target should be **UP**;
`redis_commands_processed_total` should increase as the helper writes to Redis.

## Tear down

```bash
docker compose down
```
