# AGENTS.md — notifier

## Architecture

- **`hub.ts`** — Pure class `Hub` with no I/O. Unit-tested independently. Tracks connected WebSocket clients and their optional jobId filters. `broadcast(event)` sends to every client whose filter is unset or matches `event.jobId`.
- **`broker.ts`** — Thin wiring: connects to RabbitMQ via amqplib, asserts the fanout exchange `events` (durable), asserts one exclusive auto-delete anonymous queue per process, binds it to `events`, and calls `onEvent` for each message. Includes retry/backoff on connect.
- **`server.ts`** — Creates an HTTP server for `/healthz` + `/readyz`, attaches a `WebSocketServer` at `/ws`, wires WebSocket lifecycle to Hub.
- **`main.ts`** — Entry point: loads config, creates Hub, starts server, connects broker (which calls `hub.broadcast`), installs SIGTERM/SIGINT handlers.
- **`config.ts`** — Reads `PORT` and `RABBITMQ_URL` from env. Throws if `RABBITMQ_URL` is missing.

## Key Invariants

1. **One exclusive, auto-delete queue per process** — bound to the fanout `events` exchange. This guarantees every notifier replica receives every event regardless of how many replicas exist. Never use a named/shared queue.
2. **Never consume the `process` work queue** — that is the worker's domain. The notifier is read-only from RabbitMQ's perspective (it only consumes from its own ephemeral queue).
3. **Hub is pure** — no side effects, no I/O. All network wiring lives in broker/server/main. This keeps Hub fully unit-testable without mocks.
4. **Readyz reflects broker state** — `/readyz` returns 503 until the broker is connected and the queue is bound. Use this for k8s startup probes.

## Testing

- `test/hub.test.ts` — Unit tests for Hub (no network). Fast, always runs.
- `test/integration.test.ts` — Starts a real RabbitMQ via testcontainers, connects the real broker + server, publishes via a separate amqplib producer, asserts WS frames arrive. Requires Docker; skipped automatically if Docker is unavailable.
