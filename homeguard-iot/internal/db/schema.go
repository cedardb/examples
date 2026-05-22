package db

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SchemaSQL is the canonical schema, embedded at build time. Edit
// schema.sql and `go build` re-bakes it.
//
//go:embed schema.sql
var SchemaSQL string

// SchemaPresent reports whether the `households` table exists. Used for a
// diagnostic log line on startup — we always call ApplySchema anyway since
// the file is idempotent (CREATE TABLE IF NOT EXISTS).
func SchemaPresent(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var present bool
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM information_schema.tables
		         WHERE table_name = 'households') = 1
	`).Scan(&present); err != nil {
		return false, fmt.Errorf("schema presence check: %w", err)
	}
	return present, nil
}

// ApplySchema runs the embedded schema.sql one statement at a time using
// the Postgres simple-query protocol (pgx.QueryExecModeSimpleProtocol).
// CedarDB doesn't accept DDL through the extended Parse/Bind/Execute path
// the same way vanilla Postgres does, so prepared-statement mode silently
// fails to apply CREATE TABLE; simple-protocol bypasses that.
func ApplySchema(ctx context.Context, pool *pgxpool.Pool) error {
	statements := splitSQLStatements(SchemaSQL)
	log.Printf("applying schema: %d statements", len(statements))
	for i, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt, pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf(
				"apply schema (statement %d of %d failed): %w\n--- failing statement ---\n%s\n",
				i+1, len(statements), err, stmt,
			)
		}
		log.Printf("  [%2d/%d] %s — ok", i+1, len(statements), firstLine(stmt))
	}
	log.Printf("schema applied")
	return nil
}

// ResetSchema drops everything and re-applies — destructive. Wired behind
// the simulator's -reset-schema flag.
func ResetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	drops := []string{
		"DROP TABLE IF EXISTS storage_samples CASCADE",
		"DROP TABLE IF EXISTS alerts          CASCADE",
		"DROP TABLE IF EXISTS events          CASCADE",
		"DROP TABLE IF EXISTS devices         CASCADE",
		"DROP TABLE IF EXISTS households      CASCADE",
		"DROP TABLE IF EXISTS device_types    CASCADE",
		"DROP TABLE IF EXISTS regions         CASCADE",
		"DROP TABLE IF EXISTS plans           CASCADE",
	}
	log.Printf("resetting schema: dropping %d tables", len(drops))
	for i, stmt := range drops {
		if _, err := pool.Exec(ctx, stmt, pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("drop %d: %w", i, err)
		}
		log.Printf("  [%d/%d] %s — ok", i+1, len(drops), stmt)
	}
	return ApplySchema(ctx, pool)
}

// splitSQLStatements strips `--` line comments and splits the script on `;`.
// Sufficient for our DDL (no string literals or function bodies with
// embedded semicolons).
func splitSQLStatements(sql string) []string {
	var clean strings.Builder
	clean.Grow(len(sql))
	for _, line := range strings.Split(sql, "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		clean.WriteString(line)
		clean.WriteByte('\n')
	}
	out := make([]string, 0, 16)
	for _, raw := range strings.Split(clean.String(), ";") {
		stmt := strings.TrimSpace(raw)
		if stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

func firstLine(stmt string) string {
	for _, line := range strings.Split(stmt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 70 {
			return line[:67] + "..."
		}
		return line
	}
	return "(empty)"
}
