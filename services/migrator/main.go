package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Migrate(ctx context.Context, dbURL string) error {
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil { return fmt.Errorf("connect: %w", err) }
	defer conn.Close(ctx)

	if _, err = conn.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz DEFAULT now())`); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil { return err }
	names := make([]string, 0, len(entries))
	for _, e := range entries { names = append(names, e.Name()) }
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err = conn.QueryRow(ctx, `SELECT exists(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists { log.Printf("skip %s (already applied)", name); continue }
		sqlBytes, err := migrations.ReadFile("migrations/" + name)
		if err != nil { return err }
		if _, err = conn.Exec(ctx, string(sqlBytes)); err != nil { return fmt.Errorf("apply %s: %w", name, err) }
		if _, err = conn.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, name); err != nil { return err }
		log.Printf("applied %s", name)
	}
	return nil
}

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" { log.Fatal("DATABASE_URL is required") }
	if err := Migrate(context.Background(), url); err != nil { log.Fatalf("migration failed: %v", err) }
	log.Println("migrations complete")
}
