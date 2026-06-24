import io
import json
import logging
import signal
import sys

import psycopg
import redis as redis_lib

from worker.clients import connect_rabbit, make_minio, make_pg, make_redis
from worker.config import Config
from worker.consumer import Deps, handle_job
from worker.health import start_health_server
from worker.processing import process_image

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
logger = logging.getLogger(__name__)

REDIS_TTL = 3600


def build_deps(cfg: Config, minio_client, pg_conn: psycopg.Connection,
               redis_client: redis_lib.Redis, channel) -> Deps:

    def get_original(key: str) -> bytes:
        response = minio_client.get_object(cfg.bucket_originals, key)
        try:
            return response.read()
        finally:
            response.close()
            response.release_conn()

    def _process(data: bytes) -> tuple[bytes, bytes]:
        return process_image(
            data,
            thumb_px=cfg.thumbnail_size,
            max_px=cfg.processed_max_size,
            watermark=cfg.watermark_text,
        )

    def put_result(key: str, data: bytes) -> None:
        minio_client.put_object(
            cfg.bucket_processed,
            key,
            io.BytesIO(data),
            length=len(data),
            content_type="image/png",
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
        redis_client.setex(f"job:{jid}", REDIS_TTL, json.dumps(snap))

    def publish_event(body: bytes) -> None:
        channel.basic_publish(exchange="events", routing_key="", body=body)

    return Deps(
        get_original=get_original,
        process=_process,
        put_result=put_result,
        update_db=update_db,
        set_cache=set_cache,
        publish_event=publish_event,
    )


def main() -> None:
    cfg = Config.from_env()

    minio_client = make_minio(cfg)
    pg_conn = make_pg(cfg)
    redis_client = make_redis(cfg)
    conn, channel = connect_rabbit(cfg)

    deps = build_deps(cfg, minio_client, pg_conn, redis_client, channel)

    # Health server: readyz checks that the rabbit connection is open
    start_health_server(cfg.health_port, is_ready=lambda: conn.is_open)

    # SIGTERM handler — stop consuming cleanly
    def _handle_sigterm(signum, frame):
        logger.info("SIGTERM received, stopping consumer")
        channel.stop_consuming()
        conn.close()
        sys.exit(0)

    signal.signal(signal.SIGTERM, _handle_sigterm)

    def _on_message(ch, method, properties, body):
        try:
            handle_job(deps, body)
            ch.basic_ack(delivery_tag=method.delivery_tag)
        except Exception:
            logger.exception("Job failed — nacking without requeue")
            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)

    channel.basic_consume(queue="process", on_message_callback=_on_message)
    logger.info("Worker started, waiting for messages")
    channel.start_consuming()


if __name__ == "__main__":
    main()
