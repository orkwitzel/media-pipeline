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
