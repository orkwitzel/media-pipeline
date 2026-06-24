# media-pipeline

Async image-processing pipeline — a learning project for Kubernetes concepts.
Upload an image; it gets resized, thumbnailed, watermarked, and stored in object storage.
Results stream back to the browser in real time via WebSocket.

App-only monorepo: five services, no compose/k8s manifests included.
See [`docs/contracts.md`](docs/contracts.md) for the cross-service API contract and
[`docs/env-reference.md`](docs/env-reference.md) for every environment variable.

---

## Architecture

```
Browser
  │
  │  HTTP /api/*   WebSocket /ws
  ▼                     ▼
┌─────────┐       ┌──────────┐
│ gateway │       │ notifier │
│ (Go)    │       │ (Node)   │
└────┬────┘       └────┬─────┘
     │ RabbitMQ        │ RabbitMQ
     │ (process queue) │ (events fanout)
     ▼                 ▲
┌──────────┐      ┌──────────┐
│  worker  │──────│  worker  │  (competing consumers)
│ (Python) │      │ (Python) │
└────┬─────┘      └──────────┘
     │
     ├── Postgres  (job state)
     ├── Redis     (job cache, TTL 3600s)
     └── MinIO     (originals / processed buckets)

                 ┌────────────────┐
  Browser ──────▶│  web (nginx)   │  /  → static SPA
                 └────────────────┘

Ingress routing:
  /        → web:80
  /api/... → gateway:8080  (prefix stripped)
  /ws      → notifier:8082 (WebSocket upgrade)
```

**Backing services (you deploy them):** RabbitMQ, Redis, Postgres, MinIO

---

## Services

| Service   | Language   | Role                                         | Default port | Scale pattern           |
|-----------|------------|----------------------------------------------|:------------:|-------------------------|
| migrator  | Go         | One-shot DB schema migration (Job / initContainer) | —      | Run-to-completion       |
| gateway   | Go         | Upload, list/status/result API               | 8080         | Stateless Deployment    |
| worker    | Python     | Image processing (resize, thumbnail, watermark) | 8081 (probe) | HPA / KEDA on `process` queue depth |
| notifier  | TypeScript | WebSocket fan-out of job events              | 8082         | Stateless Deployment    |
| web       | TypeScript | SPA (React + Vite) served by nginx           | 80           | Stateless Deployment    |

---

## Build all images

```bash
for s in migrator gateway worker notifier web; do
  docker build -t media-pipeline/$s ./services/$s
done
```

---

## Kubernetes concepts checklist

These concepts are exercised or required when deploying this pipeline:

- **Deployments / replicas** — gateway, worker, notifier, web each run as a Deployment; set `replicas` for horizontal scale.
- **StatefulSets + PVC** — Postgres and MinIO need stable storage; use StatefulSets with PersistentVolumeClaims.
- **Services: ClusterIP vs exposed** — gateway, notifier, and web get Services; only web (or an Ingress) is exposed externally. Postgres/Redis/RabbitMQ/MinIO use ClusterIP-only Services.
- **Ingress path routing + WebSocket** — a single Ingress routes `/` → web, `/api/` → gateway (path strip), `/ws` → notifier with WebSocket upgrade annotation.
- **ConfigMaps / Secrets** — non-sensitive config (bucket names, ports) in a ConfigMap; credentials (`DATABASE_URL`, `RABBITMQ_URL`, S3 keys, etc.) in Secrets.
- **Liveness / readiness probes** — all app services expose `/healthz` (liveness) and `/readyz` (readiness) HTTP endpoints.
- **HPA / KEDA on queue depth** — worker autoscales based on RabbitMQ `process` queue depth; KEDA's RabbitMQ scaler or a custom HPA metric.
- **Resource limits** — set `requests` and `limits` on all containers; worker is CPU/memory-intensive during image processing.
- **Graceful shutdown** — all services handle `SIGTERM`; `SHUTDOWN_TIMEOUT_SECONDS` (default 25) controls drain time.
- **Migrator as Job / initContainer** — run the migrator once as a Kubernetes Job, or as an initContainer on the gateway Deployment, before app services start.
- **NetworkPolicies (optional)** — restrict pod-to-pod traffic: only gateway/worker may reach Postgres; only worker may write to MinIO processed bucket; notifier only needs RabbitMQ.

---

## Further reading

- [`docs/contracts.md`](docs/contracts.md) — message shapes, HTTP API, WebSocket protocol, Postgres schema, Redis key format, Ingress routing.
- [`docs/env-reference.md`](docs/env-reference.md) — every environment variable for every service.
- [`docs/smoke-test.md`](docs/smoke-test.md) — ordered in-cluster smoke-test checklist.
- Each service has its own `README.md` and `AGENTS.md` under `services/<name>/`.
