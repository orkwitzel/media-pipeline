package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgStore struct{ pool *pgxpool.Pool }

func NewStore(ctx context.Context, url string) (*PgStore, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &PgStore{pool: pool}, nil
}

func (s *PgStore) Insert(ctx context.Context, j JobSnapshot) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO jobs (id, status, original_key, thumbnail_key, processed_key, error, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		j.ID, j.Status, j.OriginalKey, j.ThumbnailKey, j.ProcessedKey, j.Error, j.CreatedAt, j.UpdatedAt,
	)
	return err
}

func (s *PgStore) Get(ctx context.Context, id string) (*JobSnapshot, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, status, original_key, thumbnail_key, processed_key, error, created_at, updated_at
		 FROM jobs WHERE id = $1`, id)
	var j JobSnapshot
	if err := row.Scan(&j.ID, &j.Status, &j.OriginalKey, &j.ThumbnailKey, &j.ProcessedKey, &j.Error, &j.CreatedAt, &j.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &j, nil
}

func (s *PgStore) List(ctx context.Context) ([]JobSnapshot, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, status, original_key, thumbnail_key, processed_key, error, created_at, updated_at
		 FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []JobSnapshot
	for rows.Next() {
		var j JobSnapshot
		if err := rows.Scan(&j.ID, &j.Status, &j.OriginalKey, &j.ThumbnailKey, &j.ProcessedKey, &j.Error, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	if jobs == nil {
		jobs = []JobSnapshot{}
	}
	return jobs, rows.Err()
}

func (s *PgStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
