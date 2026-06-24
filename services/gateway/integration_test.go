//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	tcgo "github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcrabbitmq "github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestIntegrationUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()

	// --- Postgres ---
	pgC, err := tcpostgres.RunContainer(ctx,
		tcgo.WithImage("postgres:16-alpine"),
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("user"),
		tcpostgres.WithPassword("pass"),
		tcgo.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	defer pgC.Terminate(ctx)
	pgHost, _ := pgC.Host(ctx)
	pgPort, _ := pgC.MappedPort(ctx, "5432")
	pgURL := fmt.Sprintf("postgres://user:pass@%s:%s/testdb?sslmode=disable", pgHost, pgPort.Port())

	// --- RabbitMQ ---
	rmqC, err := tcrabbitmq.RunContainer(ctx,
		tcgo.WithImage("rabbitmq:3-management-alpine"),
		tcgo.WithWaitStrategy(
			wait.ForLog("Server startup complete").WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("rabbitmq: %v", err)
	}
	defer rmqC.Terminate(ctx)
	rmqHost, _ := rmqC.Host(ctx)
	rmqPort, _ := rmqC.MappedPort(ctx, "5672")
	rmqURL := fmt.Sprintf("amqp://guest:guest@%s:%s/", rmqHost, rmqPort.Port())

	// --- Redis ---
	redisC, err := tcredis.RunContainer(ctx,
		tcgo.WithImage("redis:7-alpine"),
	)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	defer redisC.Terminate(ctx)
	redisURL, err := redisC.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis conn: %v", err)
	}

	// --- MinIO ---
	minioC, err := tcminio.RunContainer(ctx,
		tcgo.WithImage("minio/minio:RELEASE.2024-01-16T16-07-38Z"),
	)
	if err != nil {
		t.Fatalf("minio: %v", err)
	}
	defer minioC.Terminate(ctx)
	minioHost, _ := minioC.Host(ctx)
	minioPort, _ := minioC.MappedPort(ctx, "9000")
	minioEndpoint := fmt.Sprintf("%s:%s", minioHost, minioPort.Port())

	// Build config
	cfg := Config{
		DatabaseURL:     pgURL,
		RabbitURL:       rmqURL,
		RedisURL:        redisURL,
		S3Endpoint:      minioEndpoint,
		S3Access:        "minioadmin",
		S3Secret:        "minioadmin",
		S3Region:        "us-east-1",
		S3UseSSL:        false,
		BucketOriginals: "originals",
		BucketProcessed: "processed",
		MaxUploadBytes:  1 << 20,
	}

	// Build adapters
	store, err := NewStore(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	obj, err := NewMinio(cfg)
	if err != nil {
		t.Fatalf("NewMinio: %v", err)
	}
	if err := obj.EnsureBuckets(ctx); err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}
	broker, err := NewBroker(cfg.RabbitURL)
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	cache, err := NewCache(cfg.RedisURL)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	// Create jobs table (same SQL as migrator/migrations/0001_init.sql)
	_, err = store.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS jobs (
			id           uuid PRIMARY KEY,
			status       text NOT NULL,
			original_key text NOT NULL,
			thumbnail_key text,
			processed_key text,
			error        text,
			created_at   timestamptz NOT NULL DEFAULT now(),
			updated_at   timestamptz NOT NULL DEFAULT now()
		)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	app := &App{Obj: obj, Store: store, Broker: broker, Cache: cache, Cfg: cfg}

	// POST upload
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "test.png")
	fw.Write([]byte("\x89PNG\r\n\x1a\nfakepng"))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202 got %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	jobID := resp["jobId"]
	if jobID == "" {
		t.Fatal("no jobId in response")
	}

	// Verify Postgres row
	job, err := store.Get(ctx, jobID)
	if err != nil || job == nil {
		t.Fatalf("job not in postgres: %v", err)
	}
	if job.Status != "pending" {
		t.Fatalf("want status=pending got %s", job.Status)
	}

	// Verify MinIO object
	_, err = obj.client.StatObject(ctx, "originals", job.OriginalKey, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("object not in MinIO: %v", err)
	}

	// Verify RabbitMQ message
	msgs, err := broker.ch.Consume(workQueue, "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	select {
	case d := <-msgs:
		var msg map[string]string
		json.Unmarshal(d.Body, &msg)
		if msg["jobId"] != jobID {
			t.Fatalf("want jobId=%s got %s", jobID, msg["jobId"])
		}
		d.Ack(false)
	case <-time.After(10 * time.Second):
		t.Fatal("no message on queue")
	}
}
