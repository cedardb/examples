// Command simulator generates the IoT event stream and writes it to
// CedarDB. Run it once per demo session; run cmd/server in parallel for
// the dashboard.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cedardb-demo/homeguard-iot/internal/db"
	"github.com/cedardb-demo/homeguard-iot/internal/sim"
)

func main() {
	householdCount := flag.Int("households", 30000, "synthesized household fleet size")
	devicesPerHH := flag.Int("devices-per-household", 10, "avg devices per household")
	tickHz := flag.Int("hz", 10, "simulator tick rate (events flush each tick)")
	rate := flag.Int("rate", 2500, "sustained events/sec target (heartbeats + triggered)")
	writers := flag.Int("writers", 8,
		"number of parallel writer goroutines; each owns a slice of the device fleet "+
			"and its own pgx conn. Bump alongside -rate to scale ingest toward 3 TB/day.")
	resetSchema := flag.Bool("reset-schema", false,
		"drop & recreate all tables on startup — destroys all data")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	log.Printf("connected to CedarDB")

	if *resetSchema {
		log.Printf("-reset-schema set: wiping all data and re-creating tables")
		if err := db.ResetSchema(ctx, pool); err != nil {
			log.Fatalf("reset schema: %v", err)
		}
	} else {
		present, err := db.SchemaPresent(ctx, pool)
		switch {
		case err != nil:
			log.Printf("schema-presence probe failed (continuing anyway): %v", err)
		case present:
			log.Printf("schema-presence probe: households table already present")
		default:
			log.Printf("schema-presence probe: households table missing")
      if err := db.ApplySchema(ctx, pool); err != nil {
			  log.Fatalf("apply schema: %v", err)
		  }
		}
	}

	s, err := sim.New(ctx, pool, *householdCount, *devicesPerHH, *tickHz, *rate, *writers)
	if err != nil {
		log.Fatalf("simulator setup: %v", err)
	}

	if err := s.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("simulator run: %v", err)
	}
	log.Printf("simulator shut down cleanly")
}
