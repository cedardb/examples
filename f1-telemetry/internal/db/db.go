// Package db wraps the connection pool to CedarDB.
//
// CedarDB speaks the Postgres wire protocol, so we use pgx unmodified. The
// only thing different from a normal Postgres setup is the DSN, which the
// caller supplies via DATABASE_URL.
package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect dials CedarDB using DATABASE_URL from the environment.
//
// Example DSN:
//
//	postgres://postgres:postgres@localhost:5432/cedar?sslmode=disable
func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// Generous pool: the simulator opens 1 long-lived conn for COPY-style
	// inserts, the web server opens several short-lived ones for dashboard
	// queries running concurrently.
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("dial CedarDB: %w", err)
	}

	// Verify the server is reachable before returning. Otherwise the first
	// real query is the one that fails, which makes errors confusing.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping CedarDB: %w", err)
	}
	return pool, nil
}
