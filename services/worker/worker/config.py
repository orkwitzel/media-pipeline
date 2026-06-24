import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Config:
    health_port: int
    database_url: str
    rabbitmq_url: str
    redis_url: str
    s3_endpoint: str
    s3_access_key: str
    s3_secret_key: str
    s3_use_ssl: bool
    s3_region: str
    bucket_originals: str
    bucket_processed: str
    prefetch: int
    thumbnail_size: int
    processed_max_size: int
    watermark_text: str

    @classmethod
    def from_env(cls) -> "Config":
        return cls(
            health_port=int(os.environ.get("HEALTH_PORT", "8081")),
            database_url=os.environ["DATABASE_URL"],
            rabbitmq_url=os.environ["RABBITMQ_URL"],
            redis_url=os.environ["REDIS_URL"],
            s3_endpoint=os.environ["S3_ENDPOINT"],
            s3_access_key=os.environ["S3_ACCESS_KEY"],
            s3_secret_key=os.environ["S3_SECRET_KEY"],
            s3_use_ssl=os.environ.get("S3_USE_SSL", "false").lower() in ("1", "true", "yes"),
            s3_region=os.environ.get("S3_REGION", "us-east-1"),
            bucket_originals=os.environ.get("BUCKET_ORIGINALS", "originals"),
            bucket_processed=os.environ.get("BUCKET_PROCESSED", "processed"),
            prefetch=int(os.environ.get("PREFETCH", "1")),
            thumbnail_size=int(os.environ.get("THUMBNAIL_SIZE", "256")),
            processed_max_size=int(os.environ.get("PROCESSED_MAX_SIZE", "1280")),
            watermark_text=os.environ.get("WATERMARK_TEXT", "media-pipeline"),
        )
