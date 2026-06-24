# AGENTS.md — worker service

## Architecture

```
main.py          — startup only: build config, wire clients, start health server, install SIGTERM handler, call channel.start_consuming()
clients.py       — thin constructors with exponential-backoff retry; no business logic
consumer.py      — pure orchestration via injected callables (Deps); zero I/O imports
processing.py    — pure image transform (Pillow only); zero I/O imports
health.py        — tiny http.server thread serving /healthz and /readyz
config.py        — frozen dataclass; reads os.environ; no side effects
```

## Key invariants

- **`processing.py` and `consumer.py` are fully unit-tested with fakes.** No real I/O ever appears in unit tests.
- **All real I/O wiring lives exclusively in `main.py` and `clients.py`.** Keep it that way.
- **Ack/nack semantics:** `basic_ack` on success; `basic_nack(requeue=False)` after publishing a failed event so the message is dead-lettered rather than looping forever.
- **`consumer.py:handle_job` always publishes a `done` or `failed` event** before returning (or raising). The caller in `main.py` nacks only after `handle_job` raises.

## THIS is the service to scale

Add replicas to increase throughput. Each replica is an independent consumer on the `process` queue with `prefetch=1`. RabbitMQ distributes messages automatically — no coordination code needed.

## Prohibited

- Never import from other services (gateway, notifier, migrator).
- Never add I/O to `processing.py` or `consumer.py`.
- Never change ack/nack to `requeue=True` — that creates infinite loops on bad messages.

## Redis contract

Key `job:{id}` → JSON snapshot, TTL 3600 s. Written by worker; read by gateway (falls back to Postgres on miss). Schema must match gateway's `GET /jobs/:id` response.

## Event contract

Exchange `events` (fanout, durable). Message shape:
```json
{ "jobId": "uuid", "status": "done|failed",
  "resultKeys": { "thumbnail": "...", "processed": "..." } | null,
  "error": null | "message" }
```
