package db

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/metadata?sslmode=disable")
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS file_metadata (
			id           TEXT PRIMARY KEY,
			filename     TEXT NOT NULL,
			size         BIGINT NOT NULL,
			content_type TEXT NOT NULL,
			location     TEXT NOT NULL,
			uploaded_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
