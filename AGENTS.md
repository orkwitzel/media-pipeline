# AGENTS.md — media-pipeline

Monorepo, polyglot microservices image pipeline. **App only** — no k8s/compose here.

## Layout
- `services/{migrator,gateway,worker,notifier,web}` — one service each, self-contained.
- `docs/contracts.md` — THE cross-service contract. Read before touching any service.
- `libs/contracts/contracts.json` — machine-readable name constants.

## Pipeline flowchart

```mermaid
flowchart TD
    Browser["Browser (user)"]

    subgraph SPA["web — nginx :8080 (SPA)"]
        Web["React SPA"]
    end

    subgraph GW["gateway — Go :8080"]
        Upload["POST /api/upload"]
        GetResult["GET /api/jobs/{id}/result"]
        GetJob["GET /api/jobs/{id}"]
    end

    subgraph Infra["Backing services"]
        MiniOOrig["MinIO — originals bucket"]
        MiniOProc["MinIO — processed bucket"]
        PG["Postgres — jobs table"]
        RMQ_Q["RabbitMQ — work queue: process\n(durable, competing consumers)"]
        RMQ_X["RabbitMQ — fanout exchange: events\n(durable)"]
        Redis["Redis — job cache\nkey: job:{id}  TTL 3600s"]
    end

    subgraph Workers["worker — Python :8081 probe\n(scale on queue depth — HPA / KEDA)"]
        W1["worker replica 1"]
        W2["worker replica 2 …"]
    end

    subgraph Notifiers["notifier — Node :8082\n(each replica binds its OWN exclusive queue)"]
        N1["notifier replica 1"]
        N2["notifier replica 2 …"]
    end

    Browser -->|"HTTP static"| Web
    Browser -->|"upload file"| Upload
    Upload -->|"store original"| MiniOOrig
    Upload -->|"INSERT status=pending"| PG
    Upload -->|"publish job message"| RMQ_Q

    RMQ_Q -->|"WORK QUEUE — one worker gets each job"| W1
    RMQ_Q -->|"WORK QUEUE — one worker gets each job"| W2

    W1 -->|"fetch original"| MiniOOrig
    W1 -->|"store processed + thumbnail"| MiniOProc
    W1 -->|"UPDATE status=done/failed"| PG
    W1 -->|"SET job snapshot (all fields)"| Redis
    W1 -->|"publish event"| RMQ_X

    RMQ_X -->|"FANOUT — every replica gets every event"| N1
    RMQ_X -->|"FANOUT — every replica gets every event"| N2

    N1 -->|"WebSocket /ws push"| Browser
    N2 -->|"WebSocket /ws push"| Browser

    Browser -->|"GET result"| GetResult
    GetResult -->|"stream image"| MiniOProc
    Browser -->|"GET status"| GetJob
    GetJob -->|"cache hit"| Redis
    GetJob -->|"cache miss → fallback"| PG

    classDef pattern fill:#fef3c7,stroke:#d97706,color:#000
    class RMQ_Q,RMQ_X pattern
```

> **Legend:**
> - **Work queue** (`process`): durable queue shared by all worker replicas — only **one** worker processes each job. Scale workers on queue depth (HPA / KEDA).
> - **Fanout exchange** (`events`): each notifier replica binds its own exclusive, auto-delete queue — **every** replica receives every event, so all connected browsers get live updates regardless of which notifier they hit.

## Invariants (do not break without updating docs/contracts.md AND every service)
- Queue `process` (durable), fanout exchange `events`, buckets `originals`/`processed`,
  Redis `job:{id}` TTL 3600. Message shapes per docs/contracts.md.
- All config via env (docs/env-reference.md). `/healthz` + `/readyz` on every service. SIGTERM-graceful.

## Working in one service
Each service builds/tests independently; you do NOT need other services' source — only the contract.
Integration tests use testcontainers (need a Docker daemon). Run a service's tests from its own dir.

## Conventions
Multi-stage non-root Dockerfiles. Structured stdout logs. TDD: failing test → minimal code → pass → commit.
