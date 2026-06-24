# Contracts

All services agree on the names and shapes below. Machine-readable mirror: `libs/contracts/contracts.json`.

## Messaging (RabbitMQ)
- **Work queue:** `process` — durable. Producer: `gateway`. Consumer: `worker` (competing consumers, `prefetch=1`). Published to the **default exchange** with routing key `process`.
- **Events exchange:** `events` — type **fanout**, durable. Producer: `worker`. Consumers: `notifier` replicas, each binding its **own exclusive, auto-delete** queue.

## Message shapes (JSON, UTF-8)
**Job** (gateway → `process`):
```json
{ "jobId": "uuid-v4", "originalKey": "originals/<uuid>.<ext>", "createdAt": "RFC3339" }
```
**Event** (worker → `events`):
```json
{ "jobId": "uuid-v4", "status": "done|failed",
  "resultKeys": { "thumbnail": "processed/<uuid>_thumb.png", "processed": "processed/<uuid>.png" },
  "error": null }
```
On failure: `status:"failed"`, `resultKeys:null`, `error:"<message>"`.

## Object storage (MinIO / S3)
- Buckets: `originals`, `processed`. Each service ensures its required buckets exist at startup.
- Keys: original `originals/<jobId>.<ext>`; results `processed/<jobId>.png` and `processed/<jobId>_thumb.png`.
- MinIO is internal only; browsers never call it. `gateway` streams results.

## Postgres (`jobs` table — owned by `migrator`)
| column         | type        | notes                              |
|----------------|-------------|------------------------------------|
| id             | uuid PK     | = jobId                            |
| status         | text        | one of pending/processing/done/failed |
| original_key   | text        |                                    |
| thumbnail_key  | text NULL   |                                    |
| processed_key  | text NULL   |                                    |
| error          | text NULL   |                                    |
| created_at     | timestamptz | default now()                      |
| updated_at     | timestamptz | default now()                      |

## Redis cache
- Key `job:{id}` → JSON status snapshot identical to gateway's `GET /jobs/:id` body. TTL 3600s. Writer: `worker`. Reader: `gateway` (falls back to Postgres on miss).

## HTTP — gateway (browser-facing via Ingress path `/api`)
- `POST /upload` — multipart form, field `file`. → `202 {"jobId":"..."}`.
- `GET  /jobs` — → `200 [{job snapshot}, ...]` (newest first).
- `GET  /jobs/{id}` — → `200 {job snapshot}` or `404`.
- `GET  /jobs/{id}/result?variant=thumbnail|processed` — streams image bytes (`Content-Type` from object) or `404`.
- `GET  /healthz` `GET /readyz`.

**Job snapshot JSON:**
```json
{ "id":"uuid","status":"pending","originalKey":"...","thumbnailKey":null,
  "processedKey":null,"error":null,"createdAt":"RFC3339","updatedAt":"RFC3339" }
```

## WebSocket — notifier (browser-facing via Ingress path `/ws`)
- Client connects `GET /ws`. Server pushes one JSON text frame per event: the Event shape above.
- Client may send `{"subscribe":"<jobId>"}` to filter; default = receive all events.
- `GET /healthz` `GET /readyz` on the same HTTP port.

## Ingress routing contract (the user wires this in k8s)
- `/`        → `web` (nginx, port 80)
- `/api/...` → `gateway` (strip `/api` prefix; e.g. `/api/upload` → gateway `/upload`)
- `/ws`      → `notifier` (WebSocket upgrade)
