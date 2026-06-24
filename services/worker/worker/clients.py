import logging
import time

import pika
import psycopg
import redis as redis_lib
from minio import Minio

from worker.config import Config

logger = logging.getLogger(__name__)

_BACKOFF = [1, 2, 4, 8, 16]


def _retry(label: str, fn, retries=5):
    for i, wait in enumerate(_BACKOFF[:retries]):
        try:
            return fn()
        except Exception as exc:
            if i + 1 >= retries:
                raise
            logger.warning("%s connection attempt %d failed: %s — retrying in %ds", label, i + 1, exc, wait)
            time.sleep(wait)


def make_minio(cfg: Config) -> Minio:
    def _connect():
        client = Minio(
            cfg.s3_endpoint,
            access_key=cfg.s3_access_key,
            secret_key=cfg.s3_secret_key,
            secure=cfg.s3_use_ssl,
            region=cfg.s3_region,
        )
        # Ensure required buckets exist
        for bucket in (cfg.bucket_originals, cfg.bucket_processed):
            if not client.bucket_exists(bucket):
                client.make_bucket(bucket)
        return client

    return _retry("MinIO", _connect)


def make_pg(cfg: Config) -> psycopg.Connection:
    def _connect():
        return psycopg.connect(cfg.database_url, autocommit=False)

    return _retry("Postgres", _connect)


def make_redis(cfg: Config) -> redis_lib.Redis:
    def _connect():
        client = redis_lib.from_url(cfg.redis_url, decode_responses=False)
        client.ping()
        return client

    return _retry("Redis", _connect)


def connect_rabbit(cfg: Config) -> tuple[pika.BlockingConnection, pika.adapters.blocking_connection.BlockingChannel]:
    def _connect():
        params = pika.URLParameters(cfg.rabbitmq_url)
        conn = pika.BlockingConnection(params)
        ch = conn.channel()
        # Declare durable work queue
        ch.queue_declare(queue="process", durable=True)
        # Declare fanout events exchange
        ch.exchange_declare(exchange="events", exchange_type="fanout", durable=True)
        # Set prefetch
        ch.basic_qos(prefetch_count=cfg.prefetch)
        return conn, ch

    return _retry("RabbitMQ", _connect)
