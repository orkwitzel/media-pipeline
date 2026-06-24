# migrator

Run-to-completion database migration service for the media-pipeline. Deploy as a Kubernetes Job or initContainer — it exits 0 on success and non-zero on failure so Kubernetes init ordering works correctly.

## Purpose

Creates and maintains the `jobs` table and a `schema_migrations` ledger table in Postgres. Migration files are applied in lexicographic order; each file is recorded in the ledger so it is never applied twice (idempotent by design).

## Environment Variables

| Variable       | Required | Description                               |
|----------------|----------|-------------------------------------------|
| `DATABASE_URL` | Yes      | Postgres connection string, e.g. `postgres://user:pass@host:5432/db?sslmode=disable` |

## Build

```bash
docker build -t media-pipeline/migrator .
```

## How Migrations Work

1. On startup, `migrator` connects to `DATABASE_URL`.
2. Creates the `schema_migrations` ledger table if it does not exist.
3. Reads all `migrations/*.sql` files embedded at build time, sorted lexicographically.
4. For each file, checks the ledger — skips if already applied, otherwise executes the SQL and records the filename.
5. Exits 0 on success, non-zero on any error.

## Running Tests

Tests use [testcontainers-go](https://golang.testcontainers.org/) and require Docker.

```bash
DOCKER_HOST=unix:///var/run/docker.sock TESTCONTAINERS_RYUK_DISABLED=true go test ./...
```

Adjust `DOCKER_HOST` to match your Docker socket path (e.g. `/Users/<you>/.rd/docker.sock` for Rancher Desktop).
