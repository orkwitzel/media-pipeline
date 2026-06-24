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
