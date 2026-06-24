# In-Cluster Smoke Test

Ordered checklist for verifying a fresh deployment.
All `kubectl` commands assume the correct namespace is set (add `-n <ns>` as needed).

---

## 1. Deploy backing services

Ensure RabbitMQ, Redis, Postgres, and MinIO are running and their Services are reachable:

```bash
kubectl get pods -l app=rabbitmq
kubectl get pods -l app=redis
kubectl get pods -l app=postgres
kubectl get pods -l app=minio
```

Wait until all pods show `Running` / `Ready`.

---

## 2. Run the migrator Job

Apply (or create) the migrator Job manifest, then confirm it completes:

```bash
kubectl apply -f k8s/migrator-job.yaml
kubectl wait --for=condition=complete job/migrator --timeout=120s
kubectl logs job/migrator
```

Expected: final log line `migrations complete` and Job status `Complete`.

---

## 3. Deploy application services

```bash
kubectl apply -f k8s/gateway.yaml
kubectl apply -f k8s/worker.yaml
kubectl apply -f k8s/notifier.yaml
kubectl apply -f k8s/web.yaml
```

Wait for rollout:

```bash
kubectl rollout status deploy/gateway
kubectl rollout status deploy/worker
kubectl rollout status deploy/notifier
kubectl rollout status deploy/web
```

---

## 4. Port-forward the web frontend

```bash
kubectl port-forward svc/web 8888:80
```

Open `http://localhost:8888` in a browser. The upload UI should load.

Alternatively port-forward the gateway directly:

```bash
kubectl port-forward svc/gateway 8080:8080
```

---

## 5. Upload an image

Replace `photo.jpg` with any local image file:

```bash
curl -s -X POST http://localhost:8080/upload \
  -F "file=@photo.jpg" | jq .
```

Expected response:

```json
{ "jobId": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" }
```

Save the `jobId` for subsequent steps.

---

## 6. Watch a worker pod process it

```bash
kubectl logs -l app=worker -f --tail=50
```

Expected log lines (within a few seconds):

```
received job <jobId>
processing <jobId>
done <jobId>
```

---

## 7. Confirm job status via API

```bash
JOB_ID=<paste jobId here>

# List all jobs (newest first)
curl -s http://localhost:8080/jobs | jq .

# Get single job status
curl -s http://localhost:8080/jobs/$JOB_ID | jq .
```

Expected single-job response when complete:

```json
{
  "id": "...",
  "status": "done",
  "originalKey": "originals/<jobId>.jpg",
  "thumbnailKey": "processed/<jobId>_thumb.png",
  "processedKey": "processed/<jobId>.png",
  "error": null,
  "createdAt": "2025-...",
  "updatedAt": "2025-..."
}
```

---

## 8. Confirm live UI update via WebSocket

In the browser at `http://localhost:8888`, upload a new image.
The job card in the UI should transition from `pending` → `processing` → `done` without a page refresh.

To verify WebSocket delivery from the CLI:

```bash
# requires wscat: npm install -g wscat
wscat -c ws://localhost:8082/ws
```

After uploading, you should receive a JSON frame matching the Event shape:

```json
{ "jobId": "...", "status": "done", "resultKeys": { "thumbnail": "...", "processed": "..." }, "error": null }
```

---

## 9. Scale worker and observe queue drain

Upload several images quickly, then scale the worker Deployment:

```bash
for i in $(seq 1 10); do
  curl -s -X POST http://localhost:8080/upload -F "file=@photo.jpg" > /dev/null
done

kubectl scale deploy/worker --replicas=5
kubectl get pods -l app=worker -w
```

In the RabbitMQ management UI (or via `kubectl exec` + `rabbitmqctl list_queues`), observe the `process` queue depth drop faster as all five replicas consume concurrently (competing-consumer pattern, `prefetch=1`).

---

## 10. Check processed bucket in MinIO

Open the MinIO console (`kubectl port-forward svc/minio 9001:9001`, then `http://localhost:9001`) or use the `mc` CLI:

```bash
mc alias set local http://localhost:9000 <access-key> <secret-key>
mc ls local/processed
```

For each completed job you should see two objects:
- `processed/<jobId>.png` — resized image (longest edge ≤ 1280 px, watermarked)
- `processed/<jobId>_thumb.png` — thumbnail (longest edge ≤ 256 px)

---

## Quick API reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/upload` | Upload image (multipart `file` field) → `202 {"jobId":"..."}` |
| `GET`  | `/jobs` | List all jobs (newest first) |
| `GET`  | `/jobs/{id}` | Single job snapshot |
| `GET`  | `/jobs/{id}/result?variant=thumbnail` | Stream thumbnail bytes |
| `GET`  | `/jobs/{id}/result?variant=processed` | Stream processed image bytes |
| `GET`  | `/healthz` | Liveness probe (gateway) |
| `GET`  | `/readyz` | Readiness probe (gateway) |
