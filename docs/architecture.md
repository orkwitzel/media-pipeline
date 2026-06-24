# Architecture

See `../README.md` for the diagram and the k8s concept checklist.
Flow: browser → web (SPA) → POST /api/upload → gateway → {MinIO original, Postgres row, RabbitMQ `process`}.
worker consumes `process` → process image → {MinIO results, Postgres update, Redis cache, RabbitMQ `events`}.
notifier consumes `events` (fanout) → WebSocket push → browser → GET /api/jobs/{id}/result via gateway.
Two messaging patterns: competing-consumers (work queue, scale workers) vs fanout (broadcast, scale notifiers).
