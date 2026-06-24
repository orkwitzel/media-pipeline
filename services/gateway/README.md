# gateway

HTTP API service for the media-pipeline.

## Endpoints

- `POST /upload` — multipart form (`file` field). Returns `202 {"jobId":"..."}`.
- `GET /jobs` — list all jobs (newest first). Returns `200 [{job snapshot}, ...]`.
- `GET /jobs/{id}` — get job by ID. Returns `200 {job snapshot}` or `404`.
- `GET /jobs/{id}/result?variant=thumbnail|processed` — stream image bytes or `404`.
- `GET /healthz` — liveness probe. Returns `200 ok`.
- `GET /readyz` — readiness probe (checks DB, Redis, RabbitMQ). Returns `200 ready` or `503`.

## Environment Variables

| var | required | default | meaning |
|-----|----------|---------|---------|
| PORT | no | 8080 | HTTP listen port |
| DATABASE_URL | yes | — | postgres DSN |
| RABBITMQ_URL | yes | — | amqp DSN |
| REDIS_URL | yes | — | redis DSN |
| S3_ENDPOINT | yes | — | MinIO/S3 host:port |
| S3_ACCESS_KEY | yes | — | |
| S3_SECRET_KEY | yes | — | |
| S3_USE_SSL | no | false | |
| S3_REGION | no | us-east-1 | |
| BUCKET_ORIGINALS | no | originals | |
| BUCKET_PROCESSED | no | processed | |
| MAX_UPLOAD_BYTES | no | 10485760 | max upload size |

## Build & Run

```bash
go build -o gateway .
./gateway
```

## Tests

Unit tests (no external deps):
```bash
go test ./...
```

Integration tests (requires Docker):
```bash
go test -tags integration ./...
```

## Notes

- MinIO is internal only; browsers never call it directly. `gateway` proxies results via `/jobs/{id}/result`.
- The `gateway` publishes to the `process` queue (RabbitMQ default exchange) on each upload.
- Redis cache (`job:{id}`) is written by `worker`; `gateway` falls back to Postgres on cache miss.
