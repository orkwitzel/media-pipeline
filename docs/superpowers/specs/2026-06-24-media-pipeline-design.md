# media-pipeline — Design Spec

**Date:** 2026-06-24
**Status:** Approved (pending written-spec review)
**Author:** brainstormed with the user (learning-by-doing k8s project)

## Purpose

A monorepo, polyglot, microservices **async image-processing pipeline**, built so that
deploying it to a Kubernetes cluster forces the operator to configure a broad surface
of k8s concepts: Deployments & replicas, StatefulSets + PVCs, Services, Ingress,
ConfigMaps/Secrets, probes, autoscaling, graceful shutdown, and (optionally) NetworkPolicies.

**Explicit scope boundary:** this repo contains **only the application** — service source,
a Dockerfile per service, per-service docs, and tests. It contains **no Kubernetes manifests
and no docker-compose file**. The user authors all cluster configuration themselves as the
learning exercise. Backing infrastructure (RabbitMQ, Redis, PostgreSQL, MinIO) is deployed
by the user from official upstream images / Helm charts; this repo only *consumes* them via
environment variables and documents exactly what it needs.

## Non-goals (YAGNI)

- No Kubernetes manifests, Helm charts, or kustomize.
- No docker-compose / local orchestration file.
- No authentication / user accounts (single shared gallery is fine for a sandbox).
- No image format conversion zoo — a small fixed set of transforms (resize, thumbnail, watermark).
- No CI/CD pipeline config (the user may add this later).
- No production hardening beyond what teaches a k8s concept (e.g. probes, graceful shutdown).

## Architecture

### App services (written here)

| Service    | Language        | Responsibility | Why this language |
|------------|-----------------|----------------|-------------------|
| `gateway`  | Go              | Public HTTP API: accept uploads → MinIO, INSERT job → Postgres, publish job → RabbitMQ; serve job list/status (Redis-cached); stream result images from MinIO. | Small static binary, idiomatic cloud-native, fast HTTP. |
| `worker`   | Python          | Consume the work queue, fetch original from MinIO, process with Pillow (resize/thumbnail/watermark), store results to MinIO, UPDATE Postgres, SET Redis cache, publish "done" event. | Pillow is the natural fit for image processing. |
| `notifier` | Node/TypeScript | Subscribe to "done" events, push live updates to browsers over WebSocket (with SSE fallback). | First-class WebSocket ecosystem. |
| `web`      | React/Vite → nginx | Upload UI + live job gallery; static assets served by nginx. | Standard SPA; nginx static serving teaches Ingress routing. |

### Backing services (deployed by the user, not in this repo)

- **RabbitMQ** — work queue (`process`) + events fanout exchange (`events`).
- **Redis** — job-status cache.
- **PostgreSQL** — job metadata (source of truth).
- **MinIO** — S3-compatible object storage; buckets `originals` and `processed`. Stays
  internal (ClusterIP only); browsers never talk to it directly — `gateway` proxies result
  images. (Presigned-URL alternative is documented but not the default.)

### Two messaging patterns (the pedagogical core)

1. **Work queue / competing consumers** — `gateway` publishes one job message to the
   `process` queue; exactly one `worker` consumes it. Scaling `worker` replicas increases
   throughput against queue depth. `prefetch=1` for fair dispatch. This is the headline
   "scale replicas" demonstration.
2. **Fan-out / broadcast** — `worker` publishes a "done" event to the `events` fanout
   exchange; every `notifier` replica binds its own exclusive/auto-delete queue and therefore
   receives every event, pushing to whichever browser connections it currently holds. This
   demonstrates why WebSocket-fronting services scale differently from stateless workers.

## Data flow

1. Browser → Ingress → `web` (static SPA) loads.
2. Browser → `POST /upload` (multipart) → `gateway`:
   - PUT original object to MinIO bucket `originals` under a generated key.
   - INSERT job row in Postgres with status `pending`.
   - Publish `{jobId, originalKey}` to RabbitMQ `process` queue.
   - Return `{jobId}` (HTTP 202).
3. `worker` consumes a `process` message:
   - GET original from MinIO.
   - Process with Pillow → produce `thumbnail` + `processed` variants.
   - PUT results to MinIO bucket `processed`.
   - UPDATE Postgres job → status `done` (+ result keys), or `failed` (+ error).
   - SET Redis cache key `job:{id}` (TTL).
   - Publish `{jobId, status, resultKeys}` to `events` fanout exchange.
   - ACK the message (or NACK/requeue on transient failure).
4. `notifier` exclusive queue receives the event → push to subscribed browser sockets.
5. Browser receives live update → `GET /jobs/:id/result?variant=thumbnail|processed` →
   `gateway` streams the object from MinIO.

## Contracts (shared, language-agnostic)

Documented in `docs/contracts.md` and, where practical, mirrored as small constant
files under `libs/contracts/` so each service references the same names.

- **Queues / exchanges:** `process` (durable work queue), `events` (fanout exchange).
- **Routing:** jobs published to default exchange with routing key `process`; events
  published to the `events` exchange (fanout, no routing key).
- **Message: job (gateway → worker)** — JSON:
  `{ "jobId": "uuid", "originalKey": "originals/<uuid>.<ext>", "createdAt": "RFC3339" }`
- **Message: event (worker → notifier)** — JSON:
  `{ "jobId": "uuid", "status": "done|failed", "resultKeys": { "thumbnail": "...", "processed": "..." } | null, "error": "string|null" }`
- **Postgres `jobs` table:** `id (uuid pk)`, `status (text)`, `original_key (text)`,
  `thumbnail_key (text null)`, `processed_key (text null)`, `error (text null)`,
  `created_at (timestamptz)`, `updated_at (timestamptz)`.
- **Redis:** `job:{id}` → JSON status snapshot, TTL ~1h.
- **Buckets:** `originals`, `processed`. Each service ensures required buckets exist on startup.

## Cross-cutting service requirements

Every app service MUST:

- Read **all** configuration from environment variables (12-factor). No config files baked in.
  Documented exhaustively in `docs/env-reference.md` and each service README.
- Expose **`/healthz`** (liveness — process is up) and **`/readyz`** (readiness — required
  dependencies reachable). The `worker` runs a tiny HTTP server purely for these probes.
- Handle **SIGTERM** gracefully: stop accepting new work, finish or requeue in-flight work,
  close broker/DB connections, exit cleanly — so k8s `terminationGracePeriod`/`preStop`
  lessons apply.
- Log structured, leveled output to stdout.
- Ship a **multi-stage Dockerfile** producing a small, non-root final image.
- Retry/back-off on startup when dependencies aren't yet reachable (so pod ordering is forgiving).

## Documentation (explicit user requirement)

Docs target **both humans and AI agents**:

- Top-level `README.md` — what the app is, the architecture diagram, how to build each image,
  the full env reference pointer, and the k8s concepts the app is designed to exercise.
- `docs/architecture.md`, `docs/contracts.md`, `docs/env-reference.md`.
- **Per-service `README.md`** — purpose, endpoints/queues, env vars, how to build/run/test.
- **`AGENTS.md`** at repo root (and per-service where useful) — machine-oriented orientation:
  where things live, the contracts, invariants, how to run tests, conventions, and gotchas,
  so an AI agent can work in any single service without reading the whole tree.

## Testing strategy

- **Unit tests** per service for core logic, with mocked dependencies (S3/broker/DB/cache).
- **Integration tests** using **testcontainers** — spin up real RabbitMQ, Redis, PostgreSQL,
  and MinIO in throwaway containers during the test run to prove cross-service wiring
  (publish→consume, store→fetch, cache, event fan-out) before deployment. Requires a Docker
  daemon available when running the integration suite; unit tests run without it.
- A documented **manual smoke-test checklist** for in-cluster verification after deploy.

## Repo layout

```
media-pipeline/
  README.md
  AGENTS.md
  docs/
    architecture.md
    contracts.md
    env-reference.md
    smoke-test.md
    superpowers/specs/2026-06-24-media-pipeline-design.md
  libs/
    contracts/                # shared constants (queue/bucket/key names) where practical
  services/
    gateway/    # Go      + Dockerfile + tests + README + AGENTS.md
    worker/     # Python  + Dockerfile + tests + README + AGENTS.md
    notifier/   # Node/TS + Dockerfile + tests + README + AGENTS.md
    web/        # React/Vite + nginx Dockerfile + README
```

## k8s concepts the app is designed to exercise

Deployments & replica scaling · StatefulSets + PVCs (Postgres, MinIO) · ClusterIP vs
externally-exposed Services · Ingress with path routing and WebSocket support (notifier) ·
ConfigMaps + Secrets · liveness/readiness probes on every workload · HorizontalPodAutoscaler
(CPU) and/or KEDA on RabbitMQ queue depth (worker) · resource requests/limits · graceful
shutdown (`terminationGracePeriod`, `preStop`) · optional NetworkPolicies (lock MinIO/Postgres
to internal traffic) · optional init-container/Job for DB migration + bucket creation.

## Open risks / decisions deferred to implementation

- Whether `gateway` runs the Postgres migration on startup or expects a separate init step —
  default: idempotent auto-migrate on startup, overridable by env flag.
- WebSocket vs SSE in `notifier` — default to WebSocket with documented SSE fallback.
- Exact image transforms/sizes — sensible defaults, configurable via env.
