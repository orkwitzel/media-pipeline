# notifier

WebSocket relay that broadcasts RabbitMQ fanout events to connected browser clients.

## Role

The notifier service bridges the RabbitMQ `events` fanout exchange and browser WebSocket clients. Each replica binds its **own exclusive, auto-delete queue** to the `events` exchange so that every replica receives every event independently — this is what makes horizontal scaling safe. If notifiers shared a single queue, only one replica would receive each event and most clients would miss it.

## WebSocket Contract

- **Endpoint:** `GET /ws` (upgraded to WebSocket)
- **Push:** One JSON text frame per event, shape:
  ```json
  { "jobId": "uuid", "status": "done|failed",
    "resultKeys": { "thumbnail": "...", "processed": "..." },
    "error": null }
  ```
- **Subscribe:** Client may send `{"subscribe":"<jobId>"}` to filter events. Default: receive all events.

## HTTP Endpoints

- `GET /healthz` — always 200
- `GET /readyz` — 200 when broker is connected, 503 otherwise

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `RABBITMQ_URL` | (required) | AMQP connection string, e.g. `amqp://guest:guest@rabbitmq:5672` |
| `PORT` | `8082` | HTTP/WebSocket listen port |

## Build & Run

```bash
npm install
npm run build      # compile TypeScript → dist/
npm start          # run dist/main.js
```

## Test

```bash
npm test           # unit tests (hub.test.ts)
# Integration test requires Docker:
DOCKER_HOST=unix:///var/run/docker.sock npm test
```

## Docker

```bash
docker build -t notifier .
docker run -e RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672 -p 8082:8082 notifier
```

## Kubernetes / Ingress Note

WebSocket connections require specific Ingress annotations to avoid timeout issues:

```yaml
nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
nginx.ingress.kubernetes.io/proxy-http-version: "1.1"
nginx.ingress.kubernetes.io/configuration-snippet: |
  proxy_set_header Upgrade $http_upgrade;
  proxy_set_header Connection "Upgrade";
```

Scale notifier replicas freely — each binds its own exclusive queue to the fanout exchange so all replicas receive all events regardless of replica count.
