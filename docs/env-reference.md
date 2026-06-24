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
