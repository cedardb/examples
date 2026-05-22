// Package db is a thin wrapper around the pgx connection pool to CedarDB.
//
// CedarDB speaks the Postgres wire protocol, so pgx works unmodified — only
// the DSN is supplied differently. The pool is sized generously so the
// simulator's writer goroutines (each holding a conn for the duration of
// a CopyFrom) don't compete with the dashboard's concurrent reads or with
// the alert resolution / armed-state shuffle background jobs.
package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect dials CedarDB using DATABASE_URL from the environment.
//
//	postgres://USER:PASS@HOST:5432/DB?sslmode=disable
func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// Sized for the high-rate demo: up to ~16 simulator writer goroutines
	// each holding a long-lived pgx conn during CopyFrom, plus the alert
	// resolver, the armed-state shuffler, the inline alert inserters, and
	// the dashboard's concurrent read paths.
	cfg.MaxConns = 64
	cfg.MinConns = 4
	cfg.MaxConnLifetime = 30 * time.Minute

	// CedarDB doesn't currently support the Postgres extended query protocol
	// (Parse / Bind / Execute), and signals SQLSTATE 08P01
	// "invalid message in simple query mode" when pgx tries to use it.
	// Forcing simple-protocol mode makes pgx inline parameters as text
	// literals and send each query as a single `Q` message — the only
	// protocol path CedarDB accepts. COPY is unaffected (it uses its own
	// dedicated protocol regardless of this setting).
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("dial CedarDB: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping CedarDB: %w", err)
	}
	return pool, nil
}
