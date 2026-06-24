# worker

Competing-consumer image processor for the media-pipeline. Reads jobs from the `process` RabbitMQ work queue, transforms images with Pillow, stores results in MinIO, updates Postgres and Redis, then publishes a fanout event on the `events` exchange. **Scale this service** — adding replicas automatically increases throughput because RabbitMQ distributes jobs across all consumers (`prefetch=1`).

## Image transforms

1. Resize original so longest edge ≤ `PROCESSED_MAX_SIZE` (default 1280 px).
2. Draw `WATERMARK_TEXT` in white at bottom-left.
3. Produce a thumbnail with longest edge ≤ `THUMBNAIL_SIZE` (default 256 px) — no watermark.
4. Both outputs are saved as PNG to `processed/<jobId>.png` and `processed/<jobId>_thumb.png`.

## Environment variables

| var | required | default | meaning |
|-----|----------|---------|---------|
| `HEALTH_PORT` | no | 8081 | probe HTTP port |
| `DATABASE_URL` | yes | — | `postgres://…` |
| `RABBITMQ_URL` | yes | — | `amqp://…` |
| `REDIS_URL` | yes | — | `redis://…` |
| `S3_ENDPOINT` | yes | — | `minio:9000` |
| `S3_ACCESS_KEY` | yes | — | MinIO access key |
| `S3_SECRET_KEY` | yes | — | MinIO secret key |
| `S3_USE_SSL` | no | false | |
| `S3_REGION` | no | us-east-1 | |
| `BUCKET_ORIGINALS` | no | originals | |
| `BUCKET_PROCESSED` | no | processed | |
| `PREFETCH` | no | 1 | RabbitMQ consumer prefetch |
| `THUMBNAIL_SIZE` | no | 256 | thumbnail longest edge, px |
| `PROCESSED_MAX_SIZE` | no | 1280 | processed longest edge, px |
| `WATERMARK_TEXT` | no | media-pipeline | watermark string |

## Build and run

```bash
# Build image (default — full TLS verification)
docker build -t media-pipeline/worker .

# Build behind a TLS-intercepting proxy (opt-in only, trusted networks)
# UV_INSECURE_HOST skips cert verification for the listed hosts during uv install.
# Use ONLY on networks you control (e.g. corporate proxy, Rancher Desktop).
docker build --build-arg UV_INSECURE_HOST="pypi.org files.pythonhosted.org" -t media-pipeline/worker .

# Run (with required env vars)
docker run --rm \
  -e DATABASE_URL=postgres://user:pass@db:5432/media \
  -e RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672/ \
  -e REDIS_URL=redis://redis:6379/0 \
  -e S3_ENDPOINT=minio:9000 \
  -e S3_ACCESS_KEY=minioadmin \
  -e S3_SECRET_KEY=minioadmin \
  media-pipeline/worker
```

## Tests

Dependencies are managed with [uv](https://docs.astral.sh/uv/). `uv sync` creates
`.venv` and installs the runtime deps plus the `dev` group (pytest, testcontainers).

```bash
# Unit tests (no Docker required)
cd services/worker
uv sync                       # creates .venv with runtime + dev deps
uv run pytest -q -m "not integration"

# Integration tests (requires Docker)
DOCKER_HOST=unix:///path/to/docker.sock TESTCONTAINERS_RYUK_DISABLED=true \
  uv run pytest -q -m integration
```

## Scaling note

Each worker replica consumes from the durable `process` queue with `prefetch=1`. RabbitMQ distributes messages in a round-robin fashion across all connected consumers. Scale horizontally on queue depth — there is no shared state between replicas.
