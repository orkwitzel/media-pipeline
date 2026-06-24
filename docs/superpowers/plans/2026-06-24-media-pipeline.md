# media-pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a polyglot, microservices async image-processing pipeline (monorepo, app-only — no k8s manifests, no docker-compose) that a learner deploys to Kubernetes themselves.

**Architecture:** A Go `gateway` accepts uploads (→ MinIO), records jobs (→ Postgres), and enqueues work (→ RabbitMQ). Python `worker` replicas consume the work queue (competing consumers), process images with Pillow, write results back, cache status in Redis, and broadcast a "done" event to a RabbitMQ fanout exchange. A Node/TS `notifier` (one exclusive queue per replica) relays those events to browsers over WebSocket. A React/nginx `web` SPA drives it. A Go `migrator` applies SQL migrations as a run-to-completion Job before app pods start.

**Tech Stack:** Go 1.22 (gateway, migrator), Python 3.12 + Pillow + pika (worker), Node 20 + TypeScript + ws + amqplib (notifier), React + Vite + nginx (web). Backing services (RabbitMQ, Redis, PostgreSQL, MinIO) are deployed by the user from upstream images and consumed via env vars.

## Parallelization Map (read this first)

```
Phase 0 (SEQUENTIAL — one agent, must finish & commit first)
  Task 0: Repo scaffold + shared contracts + docs skeleton

Phase 1 (PARALLEL — dispatch up to 5 agents at once; none depend on each other,
         all depend ONLY on Phase 0 contracts)
  Task 1: migrator   (Go)
  Task 2: gateway    (Go)
  Task 3: worker     (Python)
  Task 4: notifier   (Node/TS)
  Task 5: web        (React/Vite/nginx)

Phase 2 (SEQUENTIAL — one agent, after Phase 1 merges)
  Task 6: Top-level README, smoke-test doc, env-reference cross-check, full build verify
```

Each Phase 1 service is self-contained: its own directory, dependency manifest, Dockerfile,
unit tests, and **testcontainers** integration tests that stand up the real backing services
it needs and exercise the *contract* directly (peer services are simulated by publishing/consuming
on the broker per `docs/contracts.md`, never by importing another service).

## Global Constraints

- **No Kubernetes manifests, no Helm, no docker-compose anywhere in the repo.** App + Dockerfiles + tests + docs only.
- **All runtime config via environment variables.** No baked-in config files. Every var documented in `docs/env-reference.md` and the service README.
- Every long-running service exposes **`GET /healthz`** (liveness) and **`GET /readyz`** (readiness; checks deps). The `worker` runs a minimal HTTP server solely for these.
- Every long-running service handles **SIGTERM** gracefully (stop intake, finish/requeue in-flight, close connections, exit 0).
- Every Dockerfile is **multi-stage** and runs as a **non-root** user with a small final image.
- Services **retry with backoff** on startup until dependencies are reachable.
- Structured, leveled logs to **stdout**.
- **Docs target humans AND AI agents:** per-service `README.md` + repo-root and per-service `AGENTS.md`.
- Contract names are fixed (copy verbatim): work queue `process`; fanout exchange `events`; buckets `originals`, `processed`; Redis key pattern `job:{id}` (TTL 3600s).
- Frontend calls **relative paths** (`/api/...`, `/ws`); path routing to services is the user's Ingress job. Document this routing contract.

---

## Task 0: Repo scaffold + shared contracts + docs skeleton

**Files:**
- Create: `.gitignore`
- Create: `docs/contracts.md`
- Create: `docs/env-reference.md`
- Create: `docs/architecture.md`
- Create: `libs/contracts/contracts.json`
- Create: `libs/contracts/README.md`
- Create: `AGENTS.md`
- Create: `README.md` (stub; finalized in Task 6)

**Interfaces:**
- Produces: `docs/contracts.md` (the canonical interface every Phase 1 task reads) and
  `libs/contracts/contracts.json` (machine-readable mirror of the fixed names).

- [ ] **Step 1: Create `.gitignore`**

```gitignore
# Go
/services/gateway/bin/
/services/migrator/bin/
# Python
__pycache__/
*.pyc
.venv/
.pytest_cache/
# Node
node_modules/
dist/
*.log
# misc
.DS_Store
.env
```

- [ ] **Step 2: Create `libs/contracts/contracts.json`** (machine-readable, language-agnostic)

```json
{
  "rabbitmq": {
    "workQueue": "process",
    "workQueueDurable": true,
    "eventsExchange": "events",
    "eventsExchangeType": "fanout"
  },
  "buckets": { "originals": "originals", "processed": "processed" },
  "redis": { "jobKeyPrefix": "job:", "jobTtlSeconds": 3600 },
  "messages": {
    "job": ["jobId", "originalKey", "createdAt"],
    "event": ["jobId", "status", "resultKeys", "error"]
  },
  "jobStatus": ["pending", "processing", "done", "failed"],
  "variants": ["thumbnail", "processed"]
}
```

- [ ] **Step 3: Create `libs/contracts/README.md`**

```markdown
# Shared Contracts

`contracts.json` is the single source of truth for cross-service names (queues, exchanges,
buckets, Redis keys, message field lists). Each service reads these values at build/runtime
or copies the constants into its own language. If you change a name here, change it in every
service. Human-readable detail lives in `../../docs/contracts.md`.
```

- [ ] **Step 4: Create `docs/contracts.md`** (the canonical human contract)

````markdown
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
````

- [ ] **Step 5: Create `docs/env-reference.md`** (filled per service; table per service)

```markdown
# Environment Variable Reference

Common: `LOG_LEVEL` (default `info`), `SHUTDOWN_TIMEOUT_SECONDS` (default `25`).

## gateway
| var | required | default | meaning |
|-----|----------|---------|---------|
| PORT | no | 8080 | HTTP listen port |
| DATABASE_URL | yes | — | `postgres://user:pass@host:5432/db?sslmode=disable` |
| RABBITMQ_URL | yes | — | `amqp://user:pass@host:5672/` |
| REDIS_URL | yes | — | `redis://host:6379/0` |
| S3_ENDPOINT | yes | — | e.g. `minio:9000` |
| S3_ACCESS_KEY | yes | — | |
| S3_SECRET_KEY | yes | — | |
| S3_USE_SSL | no | false | |
| S3_REGION | no | us-east-1 | |
| BUCKET_ORIGINALS | no | originals | |
| BUCKET_PROCESSED | no | processed | |
| MAX_UPLOAD_BYTES | no | 10485760 | reject larger uploads |

## worker
| var | required | default | meaning |
|-----|----------|---------|---------|
| HEALTH_PORT | no | 8081 | probe HTTP port |
| DATABASE_URL | yes | — | |
| RABBITMQ_URL | yes | — | |
| REDIS_URL | yes | — | |
| S3_ENDPOINT / S3_ACCESS_KEY / S3_SECRET_KEY / S3_USE_SSL / S3_REGION | yes/no | — | as gateway |
| BUCKET_ORIGINALS / BUCKET_PROCESSED | no | originals / processed | |
| PREFETCH | no | 1 | RabbitMQ consumer prefetch |
| THUMBNAIL_SIZE | no | 256 | longest edge, px |
| PROCESSED_MAX_SIZE | no | 1280 | longest edge, px |
| WATERMARK_TEXT | no | media-pipeline | watermark string |

## notifier
| var | required | default | meaning |
|-----|----------|---------|---------|
| PORT | no | 8082 | HTTP + WS port |
| RABBITMQ_URL | yes | — | |

## migrator
| var | required | default | meaning |
|-----|----------|---------|---------|
| DATABASE_URL | yes | — | |

## web (build-time)
| var | required | default | meaning |
|-----|----------|---------|---------|
| (none) | | | calls relative `/api` and `/ws`; routing is Ingress's job |
```

- [ ] **Step 6: Create `docs/architecture.md`**

```markdown
# Architecture

See `../README.md` for the diagram and the k8s concept checklist.
Flow: browser → web (SPA) → POST /api/upload → gateway → {MinIO original, Postgres row, RabbitMQ `process`}.
worker consumes `process` → process image → {MinIO results, Postgres update, Redis cache, RabbitMQ `events`}.
notifier consumes `events` (fanout) → WebSocket push → browser → GET /api/jobs/{id}/result via gateway.
Two messaging patterns: competing-consumers (work queue, scale workers) vs fanout (broadcast, scale notifiers).
```

- [ ] **Step 7: Create repo-root `AGENTS.md`**

```markdown
# AGENTS.md — media-pipeline

Monorepo, polyglot microservices image pipeline. **App only** — no k8s/compose here.

## Layout
- `services/{migrator,gateway,worker,notifier,web}` — one service each, self-contained.
- `docs/contracts.md` — THE cross-service contract. Read before touching any service.
- `libs/contracts/contracts.json` — machine-readable name constants.

## Invariants (do not break without updating docs/contracts.md AND every service)
- Queue `process` (durable), fanout exchange `events`, buckets `originals`/`processed`,
  Redis `job:{id}` TTL 3600. Message shapes per docs/contracts.md.
- All config via env (docs/env-reference.md). `/healthz` + `/readyz` on every service. SIGTERM-graceful.

## Working in one service
Each service builds/tests independently; you do NOT need other services' source — only the contract.
Integration tests use testcontainers (need a Docker daemon). Run a service's tests from its own dir.

## Conventions
Multi-stage non-root Dockerfiles. Structured stdout logs. TDD: failing test → minimal code → pass → commit.
```

- [ ] **Step 8: Create `README.md` stub**

```markdown
# media-pipeline

Async image-processing pipeline for learning Kubernetes. App-only monorepo — you deploy it.
Full README finalized in the last build step. See `docs/architecture.md` and `docs/contracts.md`.
```

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "scaffold: repo structure, shared contracts, docs skeleton"
```

---

## Task 1: migrator (Go, run-to-completion init service)

**Files:**
- Create: `services/migrator/go.mod`, `services/migrator/main.go`
- Create: `services/migrator/migrations/0001_init.sql`
- Create: `services/migrator/main_test.go`
- Create: `services/migrator/Dockerfile`, `services/migrator/README.md`, `services/migrator/AGENTS.md`

**Interfaces:**
- Consumes: `DATABASE_URL`; the `jobs` schema from `docs/contracts.md`.
- Produces: the `jobs` table + a `schema_migrations` ledger. No runtime API.

- [ ] **Step 1: Init module**

Run: `cd services/migrator && go mod init media-pipeline/migrator && go get github.com/jackc/pgx/v5@latest`
Expected: `go.mod` + `go.sum` created.

- [ ] **Step 2: Create `migrations/0001_init.sql`**

```sql
CREATE TABLE IF NOT EXISTS jobs (
  id            uuid PRIMARY KEY,
  status        text NOT NULL,
  original_key  text NOT NULL,
  thumbnail_key text,
  processed_key text,
  error         text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS jobs_created_at_idx ON jobs (created_at DESC);
```

- [ ] **Step 3: Write the failing test** (`main_test.go`) — testcontainers Postgres, run `Migrate`, assert table exists and is idempotent.

```go
package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startPG(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env:          map[string]string{"POSTGRES_PASSWORD": "pass", "POSTGRES_USER": "user", "POSTGRES_DB": "app"},
		WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil { t.Fatalf("start pg: %v", err) }
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5432")
	return "postgres://user:pass@" + host + ":" + port.Port() + "/app?sslmode=disable"
}

func TestMigrateCreatesJobsTableIdempotently(t *testing.T) {
	url := startPG(t)
	if err := Migrate(context.Background(), url); err != nil { t.Fatalf("first migrate: %v", err) }
	if err := Migrate(context.Background(), url); err != nil { t.Fatalf("second migrate (idempotent): %v", err) }

	conn, err := pgx.Connect(context.Background(), url)
	if err != nil { t.Fatalf("connect: %v", err) }
	defer conn.Close(context.Background())
	var n int
	err = conn.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name='jobs'`).Scan(&n)
	if err != nil || n != 1 { t.Fatalf("jobs table missing: n=%d err=%v", n, err) }
}
```

- [ ] **Step 4: Run test, verify it fails**

Run: `cd services/migrator && go test ./...`
Expected: FAIL — `undefined: Migrate`.

- [ ] **Step 5: Implement `main.go`** (embeds SQL files, applies in order, records ledger)

```go
package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Migrate(ctx context.Context, dbURL string) error {
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil { return fmt.Errorf("connect: %w", err) }
	defer conn.Close(ctx)

	if _, err = conn.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz DEFAULT now())`); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil { return err }
	names := make([]string, 0, len(entries))
	for _, e := range entries { names = append(names, e.Name()) }
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err = conn.QueryRow(ctx, `SELECT exists(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists { log.Printf("skip %s (already applied)", name); continue }
		sqlBytes, err := migrations.ReadFile("migrations/" + name)
		if err != nil { return err }
		if _, err = conn.Exec(ctx, string(sqlBytes)); err != nil { return fmt.Errorf("apply %s: %w", name, err) }
		if _, err = conn.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, name); err != nil { return err }
		log.Printf("applied %s", name)
	}
	return nil
}

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" { log.Fatal("DATABASE_URL is required") }
	if err := Migrate(context.Background(), url); err != nil { log.Fatalf("migration failed: %v", err) }
	log.Println("migrations complete")
}
```

- [ ] **Step 6: Run test, verify it passes**

Run: `cd services/migrator && go test ./...`
Expected: PASS (needs Docker running).

- [ ] **Step 7: Create `Dockerfile`**

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/migrator .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/migrator /migrator
USER nonroot:nonroot
ENTRYPOINT ["/migrator"]
```

- [ ] **Step 8: Create `README.md` and `AGENTS.md`**

`README.md`: purpose (run-to-completion DB migrator; deploy as k8s Job or initContainer), env (`DATABASE_URL`), build (`docker build -t media-pipeline/migrator .`), how migrations work (ordered `migrations/*.sql`, ledger table), test (`go test ./...`, needs Docker).
`AGENTS.md`: owns the `jobs` schema (source of truth — see `docs/contracts.md`); add new migrations as `NNNN_name.sql`, never edit applied ones; must stay idempotent; exits 0 on success, non-zero on failure so k8s init ordering works.

- [ ] **Step 9: Commit**

```bash
git add services/migrator
git commit -m "feat(migrator): idempotent SQL migrator init service with testcontainers test"
```

---

## Task 2: gateway (Go HTTP API)

**Files:**
- Create: `services/gateway/go.mod`, `main.go`, `config.go`, `store.go` (Postgres), `objstore.go` (MinIO), `broker.go` (RabbitMQ), `cache.go` (Redis), `handlers.go`, `health.go`
- Create: `services/gateway/handlers_test.go`, `services/gateway/integration_test.go`
- Create: `services/gateway/Dockerfile`, `README.md`, `AGENTS.md`

**Interfaces:**
- Consumes: contracts (queue `process`, buckets, Redis `job:{id}`, `jobs` table, job snapshot JSON), env from `docs/env-reference.md`.
- Produces: the HTTP API in `docs/contracts.md`. The `JobSnapshot` struct other tests rely on:
  `type JobSnapshot struct { ID, Status, OriginalKey string; ThumbnailKey, ProcessedKey, Error *string; CreatedAt, UpdatedAt time.Time }` with JSON tags `id,status,originalKey,thumbnailKey,processedKey,error,createdAt,updatedAt`.

- [ ] **Step 1: Init module + deps**

Run:
```
cd services/gateway && go mod init media-pipeline/gateway
go get github.com/jackc/pgx/v5/pgxpool github.com/redis/go-redis/v9 \
  github.com/minio/minio-go/v7 github.com/rabbitmq/amqp091-go \
  github.com/google/uuid github.com/testcontainers/testcontainers-go
```

- [ ] **Step 2: Write a failing unit test for the upload handler** (`handlers_test.go`) using in-package fakes for the dependency interfaces.

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeObj struct{ put map[string][]byte }
func (f *fakeObj) Put(_ context.Context, bucket, key, ct string, b []byte) error { f.put[key] = b; return nil }
func (f *fakeObj) Get(_ context.Context, bucket, key string) ([]byte, string, error) { return f.put[key], "image/png", nil }
func (f *fakeObj) EnsureBuckets(context.Context) error { return nil }

type fakeStore struct{ inserted *JobSnapshot }
func (f *fakeStore) Insert(_ context.Context, j JobSnapshot) error { f.inserted = &j; return nil }
func (f *fakeStore) Get(context.Context, string) (*JobSnapshot, error) { return f.inserted, nil }
func (f *fakeStore) List(context.Context) ([]JobSnapshot, error) { return []JobSnapshot{*f.inserted}, nil }
func (f *fakeStore) Ping(context.Context) error { return nil }

type fakeBroker struct{ published [][]byte }
func (f *fakeBroker) PublishJob(_ context.Context, b []byte) error { f.published = append(f.published, b); return nil }
func (f *fakeBroker) Ping() error { return nil }

type fakeCache struct{}
func (fakeCache) GetJob(context.Context, string) (*JobSnapshot, error) { return nil, nil }
func (fakeCache) Ping(context.Context) error { return nil }

func TestUploadStoresEnqueuesAndReturns202(t *testing.T) {
	obj := &fakeObj{put: map[string][]byte{}}
	st := &fakeStore{}
	br := &fakeBroker{}
	app := &App{Obj: obj, Store: st, Broker: br, Cache: fakeCache{}, Cfg: Config{BucketOriginals: "originals", MaxUploadBytes: 1 << 20}}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("file", "pic.png")
	fw.Write([]byte("\x89PNGfakebytes"))
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted { t.Fatalf("want 202 got %d: %s", rec.Code, rec.Body) }
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["jobId"] == "" { t.Fatal("no jobId returned") }
	if st.inserted == nil || st.inserted.Status != "pending" { t.Fatal("job not inserted as pending") }
	if len(obj.put) != 1 { t.Fatal("original not stored") }
	if len(br.published) != 1 { t.Fatal("job not published") }
}
```

- [ ] **Step 3: Run test, verify it fails**

Run: `cd services/gateway && go test ./...`
Expected: FAIL — `App`, `Config`, `JobSnapshot`, `Router` undefined.

- [ ] **Step 4: Define types, interfaces, config** (`config.go`)

```go
package main

import (
	"context"
	"os"
	"strconv"
	"time"
)

type JobSnapshot struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"`
	OriginalKey  string    `json:"originalKey"`
	ThumbnailKey *string   `json:"thumbnailKey"`
	ProcessedKey *string   `json:"processedKey"`
	Error        *string   `json:"error"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ObjStore interface {
	Put(ctx context.Context, bucket, key, contentType string, b []byte) error
	Get(ctx context.Context, bucket, key string) ([]byte, string, error)
	EnsureBuckets(ctx context.Context) error
}
type Store interface {
	Insert(ctx context.Context, j JobSnapshot) error
	Get(ctx context.Context, id string) (*JobSnapshot, error)
	List(ctx context.Context) ([]JobSnapshot, error)
	Ping(ctx context.Context) error
}
type Broker interface {
	PublishJob(ctx context.Context, body []byte) error
	Ping() error
}
type Cache interface {
	GetJob(ctx context.Context, id string) (*JobSnapshot, error)
	Ping(ctx context.Context) error
}

type Config struct {
	Port, DatabaseURL, RabbitURL, RedisURL          string
	S3Endpoint, S3Access, S3Secret, S3Region        string
	S3UseSSL                                        bool
	BucketOriginals, BucketProcessed                string
	MaxUploadBytes                                  int64
}

func env(k, def string) string { if v := os.Getenv(k); v != "" { return v }; return def }

func LoadConfig() Config {
	max, _ := strconv.ParseInt(env("MAX_UPLOAD_BYTES", "10485760"), 10, 64)
	return Config{
		Port: env("PORT", "8080"), DatabaseURL: os.Getenv("DATABASE_URL"),
		RabbitURL: os.Getenv("RABBITMQ_URL"), RedisURL: os.Getenv("REDIS_URL"),
		S3Endpoint: os.Getenv("S3_ENDPOINT"), S3Access: os.Getenv("S3_ACCESS_KEY"),
		S3Secret: os.Getenv("S3_SECRET_KEY"), S3Region: env("S3_REGION", "us-east-1"),
		S3UseSSL: env("S3_USE_SSL", "false") == "true",
		BucketOriginals: env("BUCKET_ORIGINALS", "originals"),
		BucketProcessed: env("BUCKET_PROCESSED", "processed"),
		MaxUploadBytes: max,
	}
}

const workQueue = "process"

func nowUTC() time.Time { return time.Now().UTC() }
```

- [ ] **Step 5: Implement `App`, router, and handlers** (`handlers.go`)

```go
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

type App struct {
	Obj    ObjStore
	Store  Store
	Broker Broker
	Cache  Cache
	Cfg    Config
}

func (a *App) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", a.handleUpload)
	mux.HandleFunc("GET /jobs", a.handleList)
	mux.HandleFunc("GET /jobs/{id}", a.handleGet)
	mux.HandleFunc("GET /jobs/{id}/result", a.handleResult)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", a.handleReady)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, a.Cfg.MaxUploadBytes)
	file, hdr, err := r.FormFile("file")
	if err != nil { http.Error(w, "missing file field", http.StatusBadRequest); return }
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil { http.Error(w, "read failed", http.StatusBadRequest); return }

	id := uuid.NewString()
	ext := strings.ToLower(path.Ext(hdr.Filename)); if ext == "" { ext = ".bin" }
	key := "originals/" + id + ext
	ct := hdr.Header.Get("Content-Type"); if ct == "" { ct = "application/octet-stream" }

	if err := a.Obj.Put(r.Context(), a.Cfg.BucketOriginals, key, ct, data); err != nil {
		http.Error(w, "store failed", http.StatusBadGateway); return
	}
	now := nowUTC()
	job := JobSnapshot{ID: id, Status: "pending", OriginalKey: key, CreatedAt: now, UpdatedAt: now}
	if err := a.Store.Insert(r.Context(), job); err != nil {
		http.Error(w, "db failed", http.StatusBadGateway); return
	}
	msg, _ := json.Marshal(map[string]string{"jobId": id, "originalKey": key, "createdAt": now.Format(time.RFC3339)})
	if err := a.Broker.PublishJob(r.Context(), msg); err != nil {
		http.Error(w, "enqueue failed", http.StatusBadGateway); return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": id})
}

func (a *App) handleList(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.Store.List(r.Context())
	if err != nil { http.Error(w, "db failed", http.StatusBadGateway); return }
	writeJSON(w, http.StatusOK, jobs)
}

func (a *App) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if c, _ := a.Cache.GetJob(r.Context(), id); c != nil { writeJSON(w, http.StatusOK, c); return }
	job, err := a.Store.Get(r.Context(), id)
	if err != nil { http.Error(w, "db failed", http.StatusBadGateway); return }
	if job == nil { http.Error(w, "not found", http.StatusNotFound); return }
	writeJSON(w, http.StatusOK, job)
}

func (a *App) handleResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	variant := r.URL.Query().Get("variant"); if variant == "" { variant = "processed" }
	job, err := a.Store.Get(r.Context(), id)
	if err != nil || job == nil { http.Error(w, "not found", http.StatusNotFound); return }
	var key *string
	if variant == "thumbnail" { key = job.ThumbnailKey } else { key = job.ProcessedKey }
	if key == nil { http.Error(w, "not ready", http.StatusNotFound); return }
	b, ct, err := a.Obj.Get(r.Context(), a.Cfg.BucketProcessed, *key)
	if err != nil { http.Error(w, "fetch failed", http.StatusBadGateway); return }
	w.Header().Set("Content-Type", ct)
	w.Write(b)
}

func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second); defer cancel()
	if err := a.Store.Ping(ctx); err != nil { http.Error(w, "db", 503); return }
	if err := a.Cache.Ping(ctx); err != nil { http.Error(w, "redis", 503); return }
	if err := a.Broker.Ping(); err != nil { http.Error(w, "rabbit", 503); return }
	w.Write([]byte("ready"))
}
```

- [ ] **Step 6: Run unit test, verify it passes**

Run: `cd services/gateway && go test -run TestUpload ./...`
Expected: PASS.

- [ ] **Step 7: Implement the real adapters** — `store.go` (pgxpool: Insert/Get/List/Ping), `objstore.go` (minio-go: Put/Get/EnsureBuckets creating buckets if absent), `broker.go` (amqp091: declare durable queue `process`, PublishJob to default exchange routing key `process`, Ping via channel), `cache.go` (go-redis: GetJob reads `job:{id}` JSON, Ping). Each implements the matching interface from `config.go`. Keep each file focused.

Key signatures to implement (so they satisfy the interfaces):
```go
// objstore.go
func NewMinio(c Config) (*Minio, error)              // dials minio-go client
func (m *Minio) EnsureBuckets(ctx context.Context) error  // creates originals & processed if missing
// store.go
func NewStore(ctx context.Context, url string) (*PgStore, error)
// broker.go
func NewBroker(url string) (*RabbitBroker, error)    // opens conn+channel, QueueDeclare("process", durable)
// cache.go
func NewCache(url string) (*RedisCache, error)
```

- [ ] **Step 8: Implement `main.go`** — load config, construct adapters with startup retry/backoff, `EnsureBuckets`, start `http.Server`, handle SIGTERM with graceful `server.Shutdown`.

```go
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func mustRetry(name string, fn func() error) {
	for i := 0; i < 30; i++ {
		if err := fn(); err == nil { return } else { log.Printf("waiting for %s: %v", name, err) }
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("%s never became ready", name)
}

func main() {
	cfg := LoadConfig()
	ctx := context.Background()
	var (store *PgStore; obj *Minio; broker *RabbitBroker; cache *RedisCache; err error)
	mustRetry("postgres", func() error { store, err = NewStore(ctx, cfg.DatabaseURL); return err })
	mustRetry("minio", func() error { obj, err = NewMinio(cfg); if err != nil { return err }; return obj.EnsureBuckets(ctx) })
	mustRetry("rabbitmq", func() error { broker, err = NewBroker(cfg.RabbitURL); return err })
	mustRetry("redis", func() error { cache, err = NewCache(cfg.RedisURL); return err })

	app := &App{Obj: obj, Store: store, Broker: broker, Cache: cache, Cfg: cfg}
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: app.Router()}

	go func() {
		log.Printf("gateway listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { log.Fatal(err) }
	}()
	stop := make(chan os.Signal, 1); signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	log.Println("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 25*time.Second); defer cancel()
	_ = srv.Shutdown(sctx)
}
```

- [ ] **Step 9: Write the integration test** (`integration_test.go`, build tag `//go:build integration`) — testcontainers for Postgres + RabbitMQ + Redis + MinIO; run the migrator SQL (copy `0001_init.sql` content or exec inline create), construct real adapters, POST a small PNG to `app.Router()`, then assert: object exists in MinIO `originals`, row exists in Postgres `pending`, one message present on queue `process` (consume it back).

```go
//go:build integration
package main
// (Spin up the four containers with testcontainers-go, build Config from mapped ports,
//  call NewStore/NewMinio/NewBroker/NewCache, EnsureBuckets, create jobs table via the
//  same CREATE TABLE as migrator/migrations/0001_init.sql, POST multipart to app.Router(),
//  assert 202 + jobId, then verify MinIO object, Postgres row status="pending",
//  and consume one delivery from queue "process" whose JSON jobId matches.)
```

Note: keep the create-table SQL identical to `services/migrator/migrations/0001_init.sql`.

- [ ] **Step 10: Run tests**

Run unit: `cd services/gateway && go test ./...` → PASS.
Run integration: `go test -tags integration ./...` (needs Docker) → PASS.

- [ ] **Step 11: Create `Dockerfile`, `README.md`, `AGENTS.md`**

`Dockerfile` (same multi-stage distroless pattern as migrator, building the gateway binary, `EXPOSE 8080`, `ENTRYPOINT ["/gateway"]`).
`README.md`: endpoints (from contracts), env table, build/run/test, note MinIO stays internal and gateway proxies results.
`AGENTS.md`: dependency interfaces live in `config.go`; handlers are pure and unit-tested with fakes; real adapters in `store.go/objstore.go/broker.go/cache.go`; integration tests behind `-tags integration`; never call other services directly — only the broker/DB/cache/object-store per `docs/contracts.md`.

- [ ] **Step 12: Commit**

```bash
git add services/gateway
git commit -m "feat(gateway): upload/list/status/result API with Postgres, MinIO, RabbitMQ, Redis"
```

---

## Task 3: worker (Python, image processor + consumer)

**Files:**
- Create: `services/worker/pyproject.toml`, `worker/__init__.py`, `worker/config.py`, `worker/processing.py`, `worker/clients.py`, `worker/consumer.py`, `worker/health.py`, `worker/main.py`
- Create: `services/worker/tests/test_processing.py`, `tests/test_consumer.py`, `tests/test_integration.py`
- Create: `services/worker/Dockerfile`, `README.md`, `AGENTS.md`

**Interfaces:**
- Consumes: messages from queue `process` (shape per contracts); reads `originals/<id>.<ext>` from MinIO; `jobs` table; writes Redis `job:{id}`; publishes events to fanout `events`.
- Produces: objects `processed/<id>.png` + `processed/<id>_thumb.png`; updates Postgres; event messages.
- Key function other steps rely on:
  `process_image(data: bytes, *, thumb_px: int, max_px: int, watermark: str) -> tuple[bytes, bytes]` returning `(processed_png, thumbnail_png)`.

- [ ] **Step 1: Create `pyproject.toml`** (deps: pillow, pika, psycopg[binary], redis, minio, boto3-free; dev: pytest, testcontainers)

```toml
[project]
name = "worker"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = ["pillow>=10", "pika>=1.3", "psycopg[binary]>=3.1", "redis>=5", "minio>=7.2"]
[project.optional-dependencies]
dev = ["pytest>=8", "testcontainers>=4"]
[tool.pytest.ini_options]
testpaths = ["tests"]
```

- [ ] **Step 2: Write the failing test for `process_image`** (`tests/test_processing.py`)

```python
import io
from PIL import Image
from worker.processing import process_image

def _png(w, h):
    buf = io.BytesIO()
    Image.new("RGB", (w, h), (10, 120, 200)).save(buf, "PNG")
    return buf.getvalue()

def test_process_image_returns_processed_and_thumbnail():
    processed, thumb = process_image(_png(2000, 1000), thumb_px=256, max_px=1280, watermark="hi")
    p = Image.open(io.BytesIO(processed))
    t = Image.open(io.BytesIO(thumb))
    assert max(p.size) <= 1280
    assert max(t.size) <= 256
    assert p.format == "PNG" and t.format == "PNG"
```

- [ ] **Step 3: Run test, verify it fails**

Run: `cd services/worker && pip install -e ".[dev]" && pytest tests/test_processing.py -q`
Expected: FAIL — `ModuleNotFoundError: worker.processing`.

- [ ] **Step 4: Implement `worker/processing.py`**

```python
import io
from PIL import Image, ImageDraw

def _resize(img: Image.Image, max_px: int) -> Image.Image:
    img = img.copy()
    img.thumbnail((max_px, max_px))
    return img

def process_image(data: bytes, *, thumb_px: int, max_px: int, watermark: str) -> tuple[bytes, bytes]:
    src = Image.open(io.BytesIO(data)).convert("RGB")
    processed = _resize(src, max_px)
    draw = ImageDraw.Draw(processed)
    draw.text((10, processed.size[1] - 20), watermark, fill=(255, 255, 255))
    thumb = _resize(src, thumb_px)
    def to_png(im: Image.Image) -> bytes:
        b = io.BytesIO(); im.save(b, "PNG"); return b.getvalue()
    return to_png(processed), to_png(thumb)
```

- [ ] **Step 5: Run test, verify it passes**

Run: `cd services/worker && pytest tests/test_processing.py -q`
Expected: PASS.

- [ ] **Step 6: Implement `worker/config.py`** (env loading: HEALTH_PORT, DATABASE_URL, RABBITMQ_URL, REDIS_URL, S3_*, buckets, PREFETCH, THUMBNAIL_SIZE, PROCESSED_MAX_SIZE, WATERMARK_TEXT) as a frozen dataclass with a `from_env()` classmethod and sane defaults from `docs/env-reference.md`.

- [ ] **Step 7: Implement `worker/clients.py`** — thin constructors: `make_minio(cfg)`, `make_pg(cfg)` (psycopg connection), `make_redis(cfg)`, `connect_rabbit(cfg)` (pika BlockingConnection, declare durable queue `process`, declare fanout exchange `events`, `basic_qos(prefetch_count=cfg.prefetch)`). Each retries with backoff.

- [ ] **Step 8: Write the failing test for the message handler** (`tests/test_consumer.py`) — `handle_job` orchestrates clients via small injected callables; assert it fetches original, stores two results, updates DB, sets cache, and publishes a `done` event. Use fakes.

```python
import json
from worker.consumer import handle_job, Deps

def test_handle_job_happy_path():
    stored, published, db, cache = {}, [], {}, {}
    deps = Deps(
        get_original=lambda key: b"PNGBYTES",
        process=lambda data: (b"PROC", b"THUMB"),
        put_result=lambda key, b: stored.__setitem__(key, b),
        update_db=lambda jid, **kw: db.update({jid: kw}),
        set_cache=lambda jid, snap: cache.__setitem__(jid, snap),
        publish_event=lambda body: published.append(json.loads(body)),
    )
    msg = json.dumps({"jobId": "abc", "originalKey": "originals/abc.png", "createdAt": "now"})
    handle_job(deps, msg.encode())

    assert "processed/abc.png" in stored and "processed/abc_thumb.png" in stored
    assert db["abc"]["status"] == "done"
    assert cache["abc"]["status"] == "done"
    assert published and published[0]["status"] == "done" and published[0]["jobId"] == "abc"
```

- [ ] **Step 9: Run test, verify it fails**

Run: `cd services/worker && pytest tests/test_consumer.py -q`
Expected: FAIL — `worker.consumer` missing.

- [ ] **Step 10: Implement `worker/consumer.py`**

```python
import json
from dataclasses import dataclass
from typing import Callable

@dataclass
class Deps:
    get_original: Callable[[str], bytes]
    process: Callable[[bytes], tuple[bytes, bytes]]
    put_result: Callable[[str, bytes], None]
    update_db: Callable[..., None]
    set_cache: Callable[[str, dict], None]
    publish_event: Callable[[bytes], None]

def handle_job(deps: Deps, body: bytes) -> None:
    msg = json.loads(body)
    jid = msg["jobId"]
    try:
        original = deps.get_original(msg["originalKey"])
        processed, thumb = deps.process(original)
        pkey, tkey = f"processed/{jid}.png", f"processed/{jid}_thumb.png"
        deps.put_result(pkey, processed)
        deps.put_result(tkey, thumb)
        deps.update_db(jid, status="done", processed_key=pkey, thumbnail_key=tkey, error=None)
        snap = {"id": jid, "status": "done", "processedKey": pkey, "thumbnailKey": tkey, "error": None}
        deps.set_cache(jid, snap)
        deps.publish_event(json.dumps({"jobId": jid, "status": "done",
            "resultKeys": {"thumbnail": tkey, "processed": pkey}, "error": None}).encode())
    except Exception as exc:  # noqa: BLE001 — convert any failure into a failed event
        deps.update_db(jid, status="failed", error=str(exc))
        deps.set_cache(jid, {"id": jid, "status": "failed", "error": str(exc)})
        deps.publish_event(json.dumps({"jobId": jid, "status": "failed",
            "resultKeys": None, "error": str(exc)}).encode())
        raise
```

- [ ] **Step 11: Run test, verify it passes**

Run: `cd services/worker && pytest tests/test_consumer.py -q`
Expected: PASS.

- [ ] **Step 12: Implement `worker/health.py`** — tiny `http.server` in a thread serving `/healthz` (always 200) and `/readyz` (200 if rabbit connection open) on `HEALTH_PORT`.

- [ ] **Step 13: Implement `worker/main.py`** — build config + clients (with backoff), wire `Deps` to real clients (get_original from MinIO, process via `process_image` with cfg sizes, put_result to MinIO `processed`, update_db via psycopg, set_cache to Redis `job:{id}` with TTL 3600, publish_event to fanout `events`), start health server, then `channel.basic_consume("process", ...)` calling `handle_job`; ack on success, `basic_nack(requeue=False)` after a failed event so it doesn't loop forever; install SIGTERM handler that stops consuming and closes the connection.

- [ ] **Step 14: Write integration test** (`tests/test_integration.py`) — testcontainers RabbitMQ + Redis + Postgres + MinIO; create `jobs` table (same SQL as migrator); seed an original object; publish a job message to `process`; run one consume cycle via the real wired `Deps`; assert results in MinIO, DB row `done`, Redis set, and an event delivered on a temp queue bound to `events`. Mark with a `@pytest.mark.integration` and skip if Docker absent.

- [ ] **Step 15: Run tests**

Run unit: `cd services/worker && pytest -q -m "not integration"` → PASS.
Run integration: `pytest -q -m integration` (needs Docker) → PASS.

- [ ] **Step 16: Create `Dockerfile`, `README.md`, `AGENTS.md`**

`Dockerfile`: multi-stage `python:3.12-slim`, install deps, non-root user, `EXPOSE 8081`, `CMD ["python","-m","worker.main"]`.
`README.md`: role (competing-consumer image processor — scale this!), env table, image transforms, build/run/test, prefetch & scaling note.
`AGENTS.md`: pure logic in `processing.py`/`consumer.py` (fully unit-tested with fakes); real wiring only in `main.py`/`clients.py`; ack/nack semantics; THIS is the service to scale on RabbitMQ queue depth; never import other services.

- [ ] **Step 17: Commit**

```bash
git add services/worker
git commit -m "feat(worker): Pillow image processor consuming work queue, publishing fanout events"
```

---

## Task 4: notifier (Node/TS, WebSocket relay)

**Files:**
- Create: `services/notifier/package.json`, `tsconfig.json`, `src/config.ts`, `src/hub.ts`, `src/broker.ts`, `src/server.ts`, `src/main.ts`
- Create: `services/notifier/test/hub.test.ts`, `test/integration.test.ts`
- Create: `services/notifier/Dockerfile`, `README.md`, `AGENTS.md`

**Interfaces:**
- Consumes: events from fanout exchange `events` (binds its own exclusive queue), env `PORT`, `RABBITMQ_URL`.
- Produces: WebSocket frames (the Event JSON) to subscribed clients; `/healthz` + `/readyz`.
- Key unit: `class Hub` with `add(ws, filterJobId?)`, `remove(ws)`, `broadcast(event: object)` — sends the event to every client whose filter is unset or matches `event.jobId`.

- [ ] **Step 1: Create `package.json`**

```json
{
  "name": "notifier",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "tsc",
    "start": "node dist/main.js",
    "test": "vitest run"
  },
  "dependencies": { "amqplib": "^0.10", "ws": "^8.18" },
  "devDependencies": {
    "@types/ws": "^8.5", "@types/node": "^20", "typescript": "^5.5", "vitest": "^2"
  }
}
```

- [ ] **Step 2: Create `tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2022", "module": "ESNext", "moduleResolution": "Bundler",
    "outDir": "dist", "strict": true, "esModuleInterop": true, "skipLibCheck": true
  },
  "include": ["src"]
}
```

- [ ] **Step 3: Write the failing test for `Hub`** (`test/hub.test.ts`)

```ts
import { describe, it, expect } from "vitest";
import { Hub } from "../src/hub.js";

function fakeWS() {
  const sent: string[] = [];
  return { sent, readyState: 1, OPEN: 1, send: (d: string) => sent.push(d) } as any;
}

describe("Hub", () => {
  it("broadcasts to unfiltered clients", () => {
    const hub = new Hub();
    const a = fakeWS();
    hub.add(a);
    hub.broadcast({ jobId: "x", status: "done" });
    expect(JSON.parse(a.sent[0]).jobId).toBe("x");
  });

  it("respects per-client job filter", () => {
    const hub = new Hub();
    const a = fakeWS(), b = fakeWS();
    hub.add(a, "job-1");
    hub.add(b, "job-2");
    hub.broadcast({ jobId: "job-1", status: "done" });
    expect(a.sent.length).toBe(1);
    expect(b.sent.length).toBe(0);
  });
});
```

- [ ] **Step 4: Run test, verify it fails**

Run: `cd services/notifier && npm install && npm test`
Expected: FAIL — cannot find `../src/hub.js`.

- [ ] **Step 5: Implement `src/hub.ts`**

```ts
import type { WebSocket } from "ws";

type Client = { ws: WebSocket; filter?: string };

export class Hub {
  private clients = new Set<Client>();

  add(ws: WebSocket, filter?: string): Client {
    const c: Client = { ws, filter };
    this.clients.add(c);
    return c;
  }
  remove(ws: WebSocket): void {
    for (const c of this.clients) if (c.ws === ws) this.clients.delete(c);
  }
  broadcast(event: { jobId: string; [k: string]: unknown }): void {
    const data = JSON.stringify(event);
    for (const c of this.clients) {
      if (c.filter && c.filter !== event.jobId) continue;
      if ((c.ws as any).readyState === (c.ws as any).OPEN) c.ws.send(data);
    }
  }
  get size(): number { return this.clients.size; }
}
```

- [ ] **Step 6: Run test, verify it passes**

Run: `cd services/notifier && npm test`
Expected: PASS.

- [ ] **Step 7: Implement `src/config.ts`** — `loadConfig()` returning `{ port: Number(process.env.PORT ?? 8082), rabbitUrl: process.env.RABBITMQ_URL! }`; throw if `RABBITMQ_URL` missing.

- [ ] **Step 8: Implement `src/broker.ts`** — `connectEvents(url, onEvent)`: amqplib connect (retry/backoff), assert fanout exchange `events` durable, assert an **exclusive, auto-delete** anonymous queue, bind it to `events`, consume and call `onEvent(JSON.parse(msg))`. Return the connection so health/shutdown can use it.

- [ ] **Step 9: Implement `src/server.ts`** — create `http.Server` with `/healthz` (200) and `/readyz` (200 if broker connected else 503); attach a `WebSocketServer({ server, path: "/ws" })`; on connection, `hub.add(ws)`, parse `{subscribe}` messages to set the client filter, `hub.remove` on close.

- [ ] **Step 10: Implement `src/main.ts`** — load config, create `Hub`, start `server` listening on `port`, `connectEvents(url, e => hub.broadcast(e))`, SIGTERM handler closing WS server + broker connection then exiting 0.

- [ ] **Step 11: Write integration test** (`test/integration.test.ts`) — testcontainers RabbitMQ; start the real server on an ephemeral port; open a `ws` client to `/ws`; publish an event to the `events` fanout exchange via a separate amqplib channel; assert the client receives a frame whose `jobId` matches. Skip if Docker absent.

- [ ] **Step 12: Run tests**

Run: `cd services/notifier && npm test` (unit) → PASS.
Run integration (needs Docker): `npm test` includes it, or `npx vitest run test/integration.test.ts` → PASS.

- [ ] **Step 13: Create `Dockerfile`, `README.md`, `AGENTS.md`**

`Dockerfile`: multi-stage — `node:20-alpine` build (`npm ci && npm run build`), runtime stage copies `dist` + production deps, non-root, `EXPOSE 8082`, `CMD ["node","dist/main.js"]`.
`README.md`: role (fanout broadcast relay — each replica gets every event), WS contract, env, build/run/test, k8s note on WebSocket Ingress + why replicas each bind their own exclusive queue (so scaling notifiers is safe).
`AGENTS.md`: `Hub` is pure & unit-tested; broker/server are thin wiring; one exclusive auto-delete queue per process bound to fanout `events`; never consume the `process` work queue (that's the worker's).

- [ ] **Step 14: Commit**

```bash
git add services/notifier
git commit -m "feat(notifier): WebSocket relay broadcasting RabbitMQ fanout events"
```

---

## Task 5: web (React + Vite, served by nginx)

**Files:**
- Create: `services/web/package.json`, `vite.config.ts`, `tsconfig.json`, `index.html`, `src/main.tsx`, `src/App.tsx`, `src/api.ts`, `src/useJobs.ts`
- Create: `services/web/test/api.test.ts`
- Create: `services/web/nginx.conf`, `services/web/Dockerfile`, `README.md`, `AGENTS.md`

**Interfaces:**
- Consumes: gateway HTTP at relative `/api` and notifier WS at relative `/ws` (per Ingress routing contract).
- Produces: a static SPA (build output `dist/`) served by nginx on port 80.

- [ ] **Step 1: Create `package.json`**

```json
{
  "name": "web",
  "private": true,
  "type": "module",
  "scripts": { "dev": "vite", "build": "tsc -b && vite build", "test": "vitest run" },
  "dependencies": { "react": "^18.3", "react-dom": "^18.3" },
  "devDependencies": {
    "@types/react": "^18.3", "@types/react-dom": "^18.3",
    "@vitejs/plugin-react": "^4.3", "typescript": "^5.5", "vite": "^5.4", "vitest": "^2"
  }
}
```

- [ ] **Step 2: Create `vite.config.ts`, `tsconfig.json`, `index.html`**

`vite.config.ts`:
```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({ plugins: [react()] });
```
`tsconfig.json`: standard React+Vite strict config (`"jsx": "react-jsx"`, `"moduleResolution": "Bundler"`, include `src`).
`index.html`: minimal `<div id="root">` + `<script type="module" src="/src/main.tsx">`.

- [ ] **Step 3: Write the failing test for `api.ts`** (`test/api.test.ts`) — `uploadFile` POSTs multipart to `/api/upload` and returns the jobId; uses a stubbed `fetch`.

```ts
import { describe, it, expect, vi } from "vitest";
import { uploadFile } from "../src/api";

describe("uploadFile", () => {
  it("POSTs to /api/upload and returns jobId", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 202, json: async () => ({ jobId: "job-9" }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const id = await uploadFile(new File(["x"], "a.png", { type: "image/png" }));
    expect(id).toBe("job-9");
    expect(fetchMock).toHaveBeenCalledWith("/api/upload", expect.objectContaining({ method: "POST" }));
  });
});
```

- [ ] **Step 4: Run test, verify it fails**

Run: `cd services/web && npm install && npm test`
Expected: FAIL — cannot find `../src/api`.

- [ ] **Step 5: Implement `src/api.ts`**

```ts
export type Job = {
  id: string; status: string;
  thumbnailKey: string | null; processedKey: string | null; error: string | null;
  createdAt: string; updatedAt: string;
};

export async function uploadFile(file: File): Promise<string> {
  const fd = new FormData();
  fd.append("file", file);
  const res = await fetch("/api/upload", { method: "POST", body: fd });
  if (!res.ok) throw new Error(`upload failed: ${res.status}`);
  return (await res.json()).jobId as string;
}

export async function listJobs(): Promise<Job[]> {
  const res = await fetch("/api/jobs");
  if (!res.ok) throw new Error(`list failed: ${res.status}`);
  return res.json();
}

export function resultURL(id: string, variant: "thumbnail" | "processed"): string {
  return `/api/jobs/${id}/result?variant=${variant}`;
}
```

- [ ] **Step 6: Run test, verify it passes**

Run: `cd services/web && npm test`
Expected: PASS.

- [ ] **Step 7: Implement `src/useJobs.ts`** — a hook that `listJobs()` on mount and opens a WebSocket to `/ws` (`new WebSocket(`ws${location.protocol === "https:" ? "s" : ""}://${location.host}/ws`)`), merging incoming events into job state by `jobId`, with auto-reconnect on close.

- [ ] **Step 8: Implement `src/App.tsx`** — a file input + upload button (calls `uploadFile`), and a gallery rendering jobs from `useJobs()`; each card shows status and, when `status==="done"`, an `<img src={resultURL(id,"thumbnail")}>` linking to the processed variant. Keep it small and readable.

- [ ] **Step 9: Implement `src/main.tsx`** — standard `createRoot(...).render(<App/>)`.

- [ ] **Step 10: Verify build + tests**

Run: `cd services/web && npm test && npm run build`
Expected: tests PASS; `dist/` produced.

- [ ] **Step 11: Create `nginx.conf`** — serve `/usr/share/nginx/html` with SPA fallback (`try_files $uri /index.html`). It does NOT proxy `/api` or `/ws` — that is the Ingress's job (documented in contracts). Listen on 80.

```nginx
server {
  listen 80;
  root /usr/share/nginx/html;
  location / { try_files $uri /index.html; }
}
```

- [ ] **Step 12: Create `Dockerfile`**

```dockerfile
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

- [ ] **Step 13: Create `README.md`, `AGENTS.md`**

`README.md`: SPA that calls relative `/api` and `/ws`; the user's Ingress must route `/`→web, `/api`→gateway, `/ws`→notifier (link to contracts); build/run/test.
`AGENTS.md`: pure logic (`api.ts`) is unit-tested; UI is thin; never hardcode service hostnames — relative paths only; routing is an Ingress concern.

- [ ] **Step 14: Commit**

```bash
git add services/web
git commit -m "feat(web): React/Vite SPA with live job gallery, served by nginx"
```

---

## Task 6: Top-level docs + full build verification (sequential, after Phase 1)

**Files:**
- Modify: `README.md` (finalize)
- Create: `docs/smoke-test.md`
- Modify: `docs/env-reference.md` (cross-check against implemented code)

**Interfaces:**
- Consumes: all five built services.
- Produces: the human-facing front door + an in-cluster verification checklist.

- [ ] **Step 1: Finalize `README.md`** — project intro; an ASCII architecture diagram (browser → web/gateway/notifier → rabbit/redis/postgres/minio → worker); the service table (name, language, role, port, scale-pattern); a "build all images" snippet:

```bash
for s in migrator gateway worker notifier web; do
  docker build -t media-pipeline/$s ./services/$s
done
```
…plus the **k8s concept checklist** from the spec (Deployments/replicas, StatefulSets+PVC for postgres & minio, Services ClusterIP vs exposed, Ingress path routing + WebSocket, ConfigMaps/Secrets, probes, HPA/KEDA on `process` queue depth, resource limits, graceful shutdown, migrator as Job/initContainer, optional NetworkPolicies), and a pointer to `docs/contracts.md` + `docs/env-reference.md`.

- [ ] **Step 2: Write `docs/smoke-test.md`** — ordered in-cluster checklist: (1) deploy backing services; (2) run `migrator` Job, confirm completion; (3) deploy gateway/worker/notifier/web; (4) `kubectl port-forward` web; (5) upload an image; (6) watch a worker pod log process it; (7) confirm live UI update; (8) `kubectl scale deploy/worker --replicas=5` and upload many, observe RabbitMQ queue drain faster; (9) check `processed` bucket in MinIO. Include the exact `curl` for `POST /api/upload` and `GET /api/jobs`.

- [ ] **Step 3: Cross-check `docs/env-reference.md`** against each service's actual `config` code; fix any drift (var names, defaults). This is a read-then-correct pass — no new vars invented.

- [ ] **Step 4: Verify every image builds**

Run:
```bash
for s in migrator gateway worker notifier web; do
  docker build -t media-pipeline/$s ./services/$s || { echo "FAILED: $s"; break; }
done
```
Expected: all five build successfully.

- [ ] **Step 5: Run every service's unit tests** (sanity gate)

Run:
```bash
(cd services/migrator && go build ./...) && \
(cd services/gateway && go test ./...) && \
(cd services/worker && pytest -q -m "not integration") && \
(cd services/notifier && npm test) && \
(cd services/web && npm test)
```
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "docs: finalize README, smoke-test checklist, env-reference cross-check"
```

---

## Self-Review (completed by plan author)

**Spec coverage:** migrator (Task 1, init-service requirement) ✓; gateway+upload/list/status/result (Task 2) ✓; worker+Pillow+competing-consumer (Task 3) ✓; notifier+fanout+WebSocket (Task 4) ✓; web SPA (Task 5) ✓; both messaging patterns (Tasks 3 & 4 + contracts) ✓; MinIO/Postgres/Redis/RabbitMQ as consumed deps, no manifests (Global Constraints, Task 0) ✓; per-service + AI-agent docs (every task's README+AGENTS.md) ✓; testcontainers integration (Tasks 1–4) ✓; env-only config, probes, SIGTERM, multi-stage non-root images (Global Constraints + each task) ✓; no compose/k8s (Global Constraints) ✓; Ingress routing contract documented (Task 0) ✓; parallel execution structure (Parallelization Map) ✓.

**Placeholder scan:** No "TBD"/"implement later". Steps that describe rather than show (Task 2 adapters, Task 3 main/clients, Task 4 broker/server, Task 5 hook/UI) are deliberate where the code is mechanical wiring against named interfaces already shown; each names exact files, functions, and the interface they satisfy. Core logic and all tests have complete code.

**Type consistency:** `JobSnapshot` field/JSON names identical across Task 2 handlers, fakes, and contracts. Message shapes (`jobId/originalKey/createdAt`, `jobId/status/resultKeys/error`) identical across Tasks 2/3/4 and `docs/contracts.md`. Names `process`, `events`, `originals`, `processed`, `job:{id}` consistent everywhere.
