# Kubernetes Learning Roadmap

A living plan for what to learn next, derived from comparing the **media-pipeline**
project (hand-written plain YAML manifests) against a production-grade Helm chart —
the **JFrog charts** (`artifactory`, `artifactory-ha`, and the `jfrog-platform`
umbrella, verified against `master`: chart appVersion 7.146.17).

**How to use this doc:** each item has a **Status**. Update it as you go. The point
is learning-by-experience: every item below is something you can practice *on this
repo* by extending `deploy/k8s/`.

**Status legend:**
- `TODO` — not started
- `LEARNING` — actively reading/experimenting
- `IN PROGRESS` — partially implemented in the repo
- `DONE` — implemented and understood
- `SKIP` — consciously deprioritized (note why)

**Last updated:** 2026-06-28

---

## Already covered by media-pipeline

These are exercised by the current repo — your "day 1" surface is solid:

| Concept | Where in repo |
|---------|---------------|
| Deployments / replicas | gateway, worker, notifier, web, redis |
| StatefulSets + PVC (`volumeClaimTemplate`) | postgres, minio, rabbitmq |
| Services: ClusterIP vs exposed | all services; backing services ClusterIP-only |
| Ingress: path routing + WebSocket upgrade | `/`→web, `/api`→gateway, `/ws`→notifier |
| ConfigMaps / Secrets | `configmap.yaml`, `secret.yaml` |
| Liveness / readiness probes | all app services (`/healthz`, `/readyz`) |
| Resource requests / limits | e.g. `minio.yaml` |
| HPA / KEDA (concept) | worker scales on `process` queue depth |
| Graceful shutdown (SIGTERM) | `SHUTDOWN_TIMEOUT_SECONDS` |
| Job (run-to-completion) | migrator |

---

## The roadmap

Ordered by learning leverage. Tier 1 first — it pulls in most of the rest.

---

### Tier 1 — Helm

**Status:** `IN PROGRESS`

**Why:** The single biggest gap. The entire project is hand-applied YAML
(`kubectl apply -f`). Real clusters are delivered via Helm. Converting this repo
into one chart forces you through almost everything a production chart is built on.

**What JFrog already has:**
- Deeply structured `values.yaml` — a `global` block plus one block per microservice
  (`artifactory`, `router`, `frontend`, `access`, `nginx`, `postgresql`, ...).
- Named templates in `templates/_helpers.tpl` — `artifactory.fullname`,
  `artifactory.serviceAccountName`, `artifactory.imagePullSecrets`, plus a separate
  `_system-yaml-render.tpl` that renders the app's `system.yaml`.
- **Sub-chart dependencies** in `Chart.yaml`: single-node chart depends on
  `postgresql 16.7.26` (gated `postgresql.enabled`); the umbrella declares 8
  (`postgresql, rabbitmq, artifactory, xray, catalog, distribution, worker, bridge`),
  each gated by `<name>.enabled`.
- **Helm hooks** (umbrella level): `upgrade-hook.yaml` (pre-upgrade guard),
  `postgres-upgrade-delete-sts-hook.yaml` (pre-upgrade, orphan-deletes old PG
  StatefulSet), `migration-hook.yaml` (pre/post-upgrade migration). Each hook ships
  its own scoped SA+Role+RoleBinding and `before-hook-creation,hook-succeeded`
  delete policy.

**What we need to do:**
- [x] Convert `deploy/k8s/` manifests into a Helm chart (`Chart.yaml`, `values.yaml`,
      `templates/`). *(done — chart at `deploy/helm/`, renders namespace-agnostic via
      `.Release.Namespace`)*
- [x] Write a `_helpers.tpl` with a `fullname` and a shared labels template. *(done)*
- [~] Replace the hand-written Postgres/RabbitMQ/Redis/MinIO manifests with sub-chart
      dependencies / operators (mirrors how the JFrog umbrella pulls in
      `postgresql`/`rabbitmq`). **Bitnami is now paywalled (2025)** — using free
      alternatives instead:
  - [x] Redis → **Valkey** sub-chart dependency (Service pinned to `redis`).
  - [x] Postgres → **CloudNativePG** operator (`Cluster` CR; `postgres-rw`/`-ro`/`-r`
        Services; enables read replicas).
  - [ ] RabbitMQ → **RabbitMQ Cluster Operator** (`RabbitmqCluster` CR) — still a
        hand-rolled StatefulSet.
  - [ ] MinIO → dependency/operator — still a hand-rolled StatefulSet.
- [x] Turn the migrator Job into a `post-install`/`post-upgrade` **hook** with a
      `before-hook-creation` delete policy. *(done — `post-` not `pre-` because the DB
      is created by this same chart, so a pre-hook would deadlock on first install)*
- [~] Parameterize replicas, image tags, and resource limits through `values.yaml`.
      *(replicas done; image tags + resource limits still hardcoded in templates)*

---

### Tier 1.5 — Release orchestration (operators + Helm)

**Status:** `TODO`

**Why:** Operators (CNPG, RabbitMQ, ingress-nginx, cert-manager) are installed as
**separate releases** from the app chart — an app chart ships only the CR *instances*,
never the CRD/controller, because the CRD must exist before the chart renders. That
creates a hard ordering requirement (platform layer before app layer). Right now that
ordering is enforced by hand. The next rung is to make it **declarative**.

**The layering (already in place):**
- Platform layer — operators: CRDs + controllers, installed once cluster-wide, own
  lifecycle, own namespace (`cnpg-system`, `rabbitmq-system`, …). The *instances* they
  manage (Postgres pods, RabbitMQ broker) still run in `media-pipeline`.
- App layer — this Helm release: ships only CRs (`Cluster`, `RabbitmqCluster`,
  `Ingress`) + the app Deployments.

**What we need to do:**
- [ ] Write a `helmfile.yaml` declaring every release (each operator + the app chart)
      with explicit `needs:` ordering, e.g. `media-pipeline` `needs: [cnpg-operator,
      rabbitmq-operator]`. One `helmfile apply` then installs them in dependency order.
- [ ] (Optional) Harden the manual path first with a `Makefile`/script that encodes the
      install order, so the ordering is visible before abstracting it away.
- [ ] Note the CRD-upgrade gotcha: Helm's `crds/` dir never upgrades CRDs. Teams that
      want CRDs Helm-managed split them into a separate CRD-only chart installed early.
      (Not needed here — operator installers ship their own CRDs.)

---

### Tier 2 — Security & resilience hygiene

Small, high-value additions to each workload.

#### 2a. RBAC (ServiceAccount + Role + RoleBinding)

**Status:** `TODO`

**Why:** Identity and least privilege. Nothing in the repo defines a ServiceAccount
or any RBAC today.

**What JFrog already has:** Opt-in ServiceAccount (`serviceAccount.create`),
namespace-scoped **Role** only (no ClusterRole) with default rules
`get/watch/list` on `services, endpoints, pods`, a RoleBinding, and
`automountServiceAccountToken` controlled on the SA (default false).

**What we need to do:**
- [ ] Create a dedicated ServiceAccount for a service (e.g. gateway).
- [ ] Add a namespace Role scoped to `get/watch/list` + a RoleBinding.
- [ ] Set `automountServiceAccountToken: false` where the token is unused.

#### 2b. Hardened securityContext

**Status:** `TODO`

**Why:** Pod security baseline. No security contexts are set today.

**What JFrog already has:** `podSecurityContext` with `runAsUser/runAsGroup/fsGroup:
1030`; container securityContext with `runAsNonRoot: true`,
`allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`,
`seccompProfile.type: RuntimeDefault`. (Note: they do **not** set
`readOnlyRootFilesystem` by default.)

**What we need to do:**
- [ ] Add `runAsNonRoot: true` + a non-root UID/GID to app containers.
- [ ] Add `fsGroup` on Postgres/MinIO/RabbitMQ pods (matters for PVC ownership).
- [ ] Add `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`,
      `seccompProfile: RuntimeDefault`.
- [ ] (Stretch) Try `readOnlyRootFilesystem: true` — note this is *not* in the JFrog
      defaults, so it's independent learning.

#### 2c. PodDisruptionBudget

**Status:** `TODO`

**Why:** Survive voluntary disruptions (node drains, cluster upgrades) without
taking all replicas of a service down at once.

**What JFrog already has:** `minAvailable`-based PDBs, with separate budgets for
primary vs member nodes in HA (`artifactory-primary-pdb.yaml`,
`artifactory-node-pdb.yaml`), plus `policy/v1beta1`→`policy/v1` version switching.

**What we need to do:**
- [ ] Add a `minAvailable` PDB for gateway / notifier / web.
- [ ] Test it: `kubectl drain` a node and watch the budget hold.

#### 2d. NetworkPolicy

**Status:** `TODO` (README already *describes* the intended policy — now implement it)

**Why:** Traffic isolation. The README scopes intent ("only gateway/worker may reach
Postgres; only worker writes to MinIO processed; notifier only needs RabbitMQ") but
no policies exist yet.

**What JFrog already has:** Opt-in `artifactory-networkpolicy.yaml` that ranges over
`.Values.networkpolicy` with per-entry Ingress/Egress policyTypes.

**What we need to do:**
- [ ] Default-deny ingress in the namespace.
- [ ] Explicit allows matching the README's stated matrix.
- [ ] Verify with a denied connection (e.g. notifier → Postgres should fail).

---

### Tier 3 — Scheduling & placement

**Status:** `TODO`

**Why:** Controls *where* pods actually land — distribution, isolation, priority.
Nothing in the repo influences scheduling today.

**What JFrog already has:**
- `nodeSelector`, `tolerations`, `topologySpreadConstraints` (all templated).
- `affinity`/`podAntiAffinity` — HA StatefulSets render `preferred`/`required`
  anti-affinity with `topologyKey: kubernetes.io/hostname` so replicas spread across
  nodes.
- `priorityClassName` plus a dedicated `PriorityClass` object
  (`artifactory-priority-class.yaml`, `globalDefault: false`, gated
  `priorityClass.create`).

**What we need to do:**
- [ ] `podAntiAffinity` to spread `worker` replicas across nodes.
- [ ] `topologySpreadConstraints` for even distribution.
- [ ] A `PriorityClass` — make Postgres higher priority than workers.
- [ ] Experiment with `nodeSelector`/`tolerations` on a multi-node cluster
      (bump Rancher Desktop / kind to 3 nodes and watch placement).

---

### Tier 4 — Operational refinements

#### 4a. startupProbe (the 3-probe model)

**Status:** `TODO`

**Why:** Repo only has liveness + readiness. Slow-booting components (Postgres,
RabbitMQ) can trip a liveness probe during startup without a startupProbe.

**What JFrog already has:** A startupProbe alongside liveness+readiness — default
httpGet with `failureThreshold: 90`, `periodSeconds: 5`.

**What we need to do:**
- [ ] Add a startupProbe to Postgres and RabbitMQ.

#### 4b. Config-checksum rolling restarts

**Status:** `TODO` — *but already bitten by this twice: swapping Redis→Valkey and
Postgres→CNPG changed the Secret/Service but did NOT restart gateway/worker, which
held stale `DATABASE_URL` + stale resolved IPs and 503'd until a manual
`kubectl rollout restart`. This item is the permanent fix.*

**Why:** Right now, editing a ConfigMap/Secret does **not** restart the pods that
consume it. A checksum annotation fixes that.

**What JFrog already has:** Pod annotations like `checksum/database-secrets`,
`checksum/binarystore`, `checksum/systemyaml`, each computed via
`include (...) | sha256sum`, so config changes force a rollout.

**What we need to do:**
- [ ] Add a `checksum/config` pod annotation (hash of the ConfigMap/Secret) to a
      Deployment so config edits trigger a rolling restart. (Pairs naturally with
      Tier 1 Helm templating.)

#### 4c. Ordered startup via initContainers

**Status:** `IN PROGRESS` (have a migrator Job; no dependency gating)

**Why:** Cleaner dependency ordering than relying on crash-loop-retry. The migrator
Job exists, but app containers don't wait for their dependencies.

**What JFrog already has:** Many init containers — `wait-for-db` (TCP-polls
PostgreSQL), `copy-system-configurations`, `access-bootstrap-creds`,
`copy-custom-certificates`, and HA `wait-for-primary` (TCP-polls the primary before
joining). Migration runs via an initContainer + ConfigMap in the single-node chart
(hooks only at the umbrella level).

**What we need to do:**
- [ ] Add a `wait-for-db` initContainer to gateway/worker that TCP-polls Postgres.
- [ ] (Optional) `wait-for-rabbitmq` / `wait-for-minio` equivalents.

#### 4d. Multi-container pods / sidecars

**Status:** `TODO`

**Why:** Repo is one-container-per-pod. The sidecar pattern is a core k8s idiom.

**What JFrog already has:** `splitServicesToContainers` runs many services as
separate containers in one pod, each with a logger tail sidecar, plus an optional
`filebeat` log-shipping sidecar (`filebeat-configmap.yaml`).

**What we need to do:**
- [ ] Add a log-shipping or metrics sidecar to one service to learn the pattern.

---

### Tier 5 — Observability (gap in BOTH projects)

**Status:** `TODO`

**Why:** Arguably the biggest missing piece overall — and notably, the JFrog chart
*also* lacks Prometheus-operator integration, so this is independent learning, not
something to copy from the chart.

**What JFrog has / lacks:** Built-in OpenMetrics endpoint (opt-in,
`artifactory.metrics.enabled`) exposed via `system.yaml`, and optional filebeat log
sidecars. **No** ServiceMonitor, **no** Prometheus-operator integration, **no**
metrics-exporter sidecar.

**What we need to do:**
- [ ] Install `kube-prometheus-stack` (Prometheus + Grafana + operator).
- [ ] Expose app metrics and add a `ServiceMonitor` to scrape them.
- [ ] Build a Grafana dashboard (e.g. queue depth vs. worker replicas).

---

### Tier 6 — Namespace governance

**Status:** `TODO`

**Why:** Cap a namespace's total resource consumption. Absent in both projects.

**What JFrog has / lacks:** **No** `ResourceQuota`, **no** `LimitRange` templates
(arbitrary extras only via a `tpl`-rendered `additional-resources.yaml` pass-through).

**What we need to do:**
- [ ] Add a `ResourceQuota` to the namespace.
- [ ] Add a `LimitRange` with default container requests/limits.

---

### Tier 7 — GitOps (Argo CD / Flux)

**Status:** `TODO`

**Why:** The production-standard way to coordinate many operators + app releases. A
controller in the cluster continuously syncs releases from git, enforces the
platform-before-app ordering automatically, and corrects drift. This is the heavyweight
big brother of Tier 1.5's Helmfile — a genuinely separate learning track (parallel to
Tier 5 observability), not something to copy from the JFrog chart.

**What we need to do:**
- [ ] Pick one (Argo CD or Flux) and install it in the cluster.
- [ ] Express the platform-before-app ordering with the tool's primitive:
      Argo CD **sync waves** + app-of-apps, or Flux `HelmRelease` `dependsOn:`.
- [ ] Point it at this repo so a git push reconciles the cluster (and watch it correct
      a manually-deleted resource — the drift-correction payoff).

---

## Explicitly NOT in the JFrog chart (don't expect to learn these from it)

So you don't over-claim what the chart demonstrates:
- ServiceMonitor / Prometheus-operator integration — Tier 5, learn separately
- Metrics-exporter sidecar
- `ResourceQuota` / `LimitRange` — Tier 6, learn separately
- `readOnlyRootFilesystem`
- A true `clusterIP: None` headless Service (interesting: they run StatefulSets but
  point `serviceName` at a regular ClusterIP Service)

---

## Recommended order

1. **Tier 1 (Helm)** — Helm-ify the repo with sub-charts/operators + migrator-as-hook.
   This naturally pulls in ~60% of everything below.
2. **Tier 1.5** — release orchestration (Helmfile): make the operator-before-app
   ordering declarative. The natural next rung straight off Tier 1.
3. **Tier 2** — security & resilience hygiene (small additions per workload).
4. **Tier 3** — scheduling (best on a multi-node cluster).
5. **Tier 4** — operational refinements.
6. **Tier 5** — observability (the big independent track).
7. **Tier 6** — namespace governance.
8. **Tier 7** — GitOps (Argo CD / Flux): the production-grade successor to Tier 1.5;
   its own track, can run in parallel with Tier 5.

**Project constraint reminder:** the app and manifests are the learning artifact —
the *cluster wiring is done by hand by you* to learn k8s configuration. Keep dev-only
secret placeholders (`app`/`app`, `minioadmin`/`minioadmin`) as placeholders; never
commit real secrets to the public repo.
