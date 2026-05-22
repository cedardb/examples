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

// SchemaSQL is the contents of the canonical schema.sql, embedded into the
// simulator binary at build time. Lives next to db.go so the //go:embed
// directive can pick it up — Go embed can only see files in the same
// package directory or below it.
//
// If you edit schema.sql you don't need to do anything else: a `go build`
// re-bakes it into the binary.
//
//go:embed schema.sql
var SchemaSQL string

// SchemaPresent returns true if the canonical `sessions` table already
// exists in the connected database. We use this as a cheap "has the schema
// been applied?" check on simulator startup, so subsequent runs don't wipe
// data when the schema is already in place.
//
// We query information_schema.tables (SQL-standard, present in CedarDB)
// rather than Postgres's `to_regclass`, which CedarDB doesn't yet expose.
func SchemaPresent(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var present bool
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM information_schema.tables
		         WHERE table_name = 'sessions') = 1
	`).Scan(&present); err != nil {
		return false, fmt.Errorf("schema presence check: %w", err)
	}
	return present, nil
}

// ApplySchema runs the embedded schema.sql against the database. The file
// is idempotent — every statement uses CREATE TABLE IF NOT EXISTS or
// CREATE INDEX IF NOT EXISTS — so it's safe to call on every cold start
// without losing prior data. For destructive re-init, call ResetSchema
// instead (drops everything first, then calls ApplySchema).
//
// Two non-obvious things going on here:
//
//   - We execute each statement separately, because CedarDB's protocol
//     implementation doesn't currently accept multi-statement queries. The
//     splitter strips `--` line comments and breaks on `;`; that's safe for
//     our DDL since none of it embeds semicolons inside string literals or
//     function bodies.
//   - We force `pgx.QueryExecModeSimpleProtocol` on each Exec. pgx's default
//     mode (CacheStatement) sends Parse → Bind → Execute through the
//     extended protocol and caches the resulting prepared statement.
//     CedarDB rejects (or, worse, silently swallows) DDL prepared that way.
//     Simple-protocol Exec just sends the SQL text as a `Q` message, which
//     CedarDB's parser handles correctly.
//
// Per-statement log lines give you visual confirmation that each piece of
// the schema was accepted; this is a once-per-cold-start operation so the
// chattiness doesn't matter.
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

// ResetSchema drops every table the demo owns and then re-applies the
// embedded schema. Wired behind the simulator's `-reset-schema` flag so
// it's never called accidentally. Each DROP is sent via the simple
// protocol for the same reason ApplySchema uses simple protocol —
// CedarDB doesn't accept DDL through prepare/bind/execute.
func ResetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	// Order matters because of foreign-key-style cascades: drop the
	// fact tables first, then the dimensions. CASCADE would handle this
	// for us with real FKs, but we don't define any, so we just sequence.
	drops := []string{
		"DROP TABLE IF EXISTS telemetry CASCADE",
		"DROP TABLE IF EXISTS laps      CASCADE",
		"DROP TABLE IF EXISTS events    CASCADE",
		"DROP TABLE IF EXISTS drivers   CASCADE",
		"DROP TABLE IF EXISTS sessions  CASCADE",
	}
	log.Printf("resetting schema: dropping %d tables", len(drops))
	for i, stmt := range drops {
		if _, err := pool.Exec(ctx, stmt, pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf(
				"reset schema (drop %d of %d failed): %w\n--- failing statement ---\n%s\n",
				i+1, len(drops), err, stmt,
			)
		}
		log.Printf("  [%d/%d] %s — ok", i+1, len(drops), stmt)
	}
	return ApplySchema(ctx, pool)
}

// firstLine returns the first non-empty line of the statement, trimmed to
// a sensible length, for the per-statement progress log.
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

// splitSQLStatements turns a SQL script into a slice of individual
// statements. Two-step process: strip `--` line comments first so semicolons
// in trailing comments don't confuse the splitter, then split on `;` and
// drop empty fragments.
//
// This is good enough for DDL that doesn't include string literals or
// PL/pgSQL function bodies with embedded `;` or `--`. The schema we ship
// satisfies that constraint; if you add `CREATE FUNCTION ... $$ ... $$`
// blocks or string DEFAULTs containing semicolons you'll need a smarter
// tokenizer here.
func splitSQLStatements(sql string) []string {
	// Strip `-- …` line comments. We keep the newline so line numbers in
	// downstream error messages still line up roughly with the source file.
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
