"""Integration test: RabbitMQ + Redis + Postgres + MinIO via testcontainers.

Requires Docker. Marked @pytest.mark.integration so it runs only when
explicitly selected: pytest -q -m integration

On Rancher Desktop, set DOCKER_HOST=unix:///Users/ork/.rd/docker.sock and
TESTCONTAINERS_RYUK_DISABLED=true before running.
"""
import io
import json
import os
import shutil

import pytest
from PIL import Image


def _png_bytes(w=800, h=600):
    buf = io.BytesIO()
    Image.new("RGB", (w, h), (50, 100, 150)).save(buf, "PNG")
    return buf.getvalue()


def _docker_available():
    """Return True if a usable Docker daemon is reachable."""
    if not shutil.which("docker"):
        return False
    try:
        import docker
        docker.from_env().version()
        return True
    except Exception:
        return False


@pytest.mark.integration
@pytest.mark.skipif(not _docker_available(), reason="Docker not available")
def test_full_pipeline():
    from testcontainers.rabbitmq import RabbitMqContainer
    from testcontainers.redis import RedisContainer
    from testcontainers.postgres import PostgresContainer
    from testcontainers.minio import MinioContainer

    import pika
    import psycopg

    from worker.consumer import Deps, handle_job
    from worker.processing import process_image

    with (
        RabbitMqContainer("rabbitmq:3.13-alpine") as rmq,
        RedisContainer("redis:7-alpine") as rd,
        PostgresContainer("postgres:16-alpine", dbname="media", driver=None) as pg,
        MinioContainer("minio/minio:latest") as minio_c,
    ):
        # ── RabbitMQ ────────────────────────────────────────────────────────
        rabbit_params = rmq.get_connection_params()
        rabbit_conn = pika.BlockingConnection(rabbit_params)
        ch = rabbit_conn.channel()
        ch.queue_declare(queue="process", durable=True)
        ch.exchange_declare(exchange="events", exchange_type="fanout", durable=True)
        # Bind a temporary queue to capture fanout events
        result = ch.queue_declare(queue="", exclusive=True)
        event_queue = result.method.queue
        ch.queue_bind(exchange="events", queue=event_queue)

        # ── Redis ────────────────────────────────────────────────────────────
        redis_client = rd.get_client(decode_responses=False)

        # ── Postgres ─────────────────────────────────────────────────────────
        pg_url = pg.get_connection_url()
        pg_conn = psycopg.connect(pg_url, autocommit=False)
        with pg_conn.cursor() as cur:
            cur.execute("""
                CREATE TABLE IF NOT EXISTS jobs (
                    id TEXT PRIMARY KEY,
                    status TEXT NOT NULL,
                    original_key TEXT,
                    thumbnail_key TEXT,
                    processed_key TEXT,
                    error TEXT,
                    created_at TIMESTAMPTZ DEFAULT now(),
                    updated_at TIMESTAMPTZ DEFAULT now()
                )
            """)
            cur.execute(
                "INSERT INTO jobs (id, status, original_key) VALUES (%s, %s, %s)",
                ("job-001", "pending", "originals/job-001.png"),
            )
        pg_conn.commit()

        # ── MinIO ─────────────────────────────────────────────────────────────
        minio_client = minio_c.get_client()
        for bucket in ("originals", "processed"):
            if not minio_client.bucket_exists(bucket):
                minio_client.make_bucket(bucket)

        # Seed original object
        png_data = _png_bytes()
        minio_client.put_object(
            "originals", "originals/job-001.png",
            io.BytesIO(png_data), length=len(png_data), content_type="image/png",
        )

        # ── Wire Deps ─────────────────────────────────────────────────────────
        published = []

        def get_original(key: str) -> bytes:
            resp = minio_client.get_object("originals", key)
            try:
                return resp.read()
            finally:
                resp.close()
                resp.release_conn()

        def put_result(key: str, data: bytes) -> None:
            minio_client.put_object(
                "processed", key,
                io.BytesIO(data), length=len(data), content_type="image/png",
            )

        def update_db(jid: str, **kwargs) -> None:
            set_clauses = ", ".join(f"{k} = %s" for k in kwargs)
            values = list(kwargs.values()) + [jid]
            with pg_conn.cursor() as cur:
                cur.execute(
                    f"UPDATE jobs SET {set_clauses}, updated_at = now() WHERE id = %s",
                    values,
                )
            pg_conn.commit()

        def set_cache(jid: str, snap: dict) -> None:
            redis_client.setex(f"job:{jid}", 3600, json.dumps(snap))

        def publish_event(body: bytes) -> None:
            ch.basic_publish(exchange="events", routing_key="", body=body)
            published.append(json.loads(body))

        deps = Deps(
            get_original=get_original,
            process=lambda data: process_image(data, thumb_px=256, max_px=1280, watermark="test"),
            put_result=put_result,
            update_db=update_db,
            set_cache=set_cache,
            publish_event=publish_event,
        )

        # ── Publish and consume one job ───────────────────────────────────────
        job_msg = json.dumps({
            "jobId": "job-001",
            "originalKey": "originals/job-001.png",
            "createdAt": "2024-01-01T00:00:00Z",
        }).encode()

        handle_job(deps, job_msg)

        # ── Assertions ────────────────────────────────────────────────────────
        # 1. Results in MinIO
        proc_resp = minio_client.get_object("processed", "processed/job-001.png")
        proc_data = proc_resp.read()
        assert len(proc_data) > 0
        proc_img = Image.open(io.BytesIO(proc_data))
        assert max(proc_img.size) <= 1280

        thumb_resp = minio_client.get_object("processed", "processed/job-001_thumb.png")
        thumb_data = thumb_resp.read()
        assert len(thumb_data) > 0
        thumb_img = Image.open(io.BytesIO(thumb_data))
        assert max(thumb_img.size) <= 256

        # 2. DB row updated to done
        with pg_conn.cursor() as cur:
            cur.execute("SELECT status, processed_key, thumbnail_key FROM jobs WHERE id = %s", ("job-001",))
            row = cur.fetchone()
        assert row is not None
        assert row[0] == "done"
        assert row[1] == "processed/job-001.png"
        assert row[2] == "processed/job-001_thumb.png"

        # 3. Redis cache set
        cached_raw = redis_client.get("job:job-001")
        assert cached_raw is not None
        cached = json.loads(cached_raw)
        assert cached["status"] == "done"

        # 4. Event published on fanout exchange
        assert len(published) == 1
        evt = published[0]
        assert evt["jobId"] == "job-001"
        assert evt["status"] == "done"
        assert evt["resultKeys"]["processed"] == "processed/job-001.png"
        assert evt["resultKeys"]["thumbnail"] == "processed/job-001_thumb.png"

        # Cleanup
        pg_conn.close()
        rabbit_conn.close()
