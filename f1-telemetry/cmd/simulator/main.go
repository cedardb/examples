// Command simulator drives the 20-car race loop and INSERTs telemetry/laps
// into CedarDB. Run this in one terminal; run cmd/server in another.
//
// Pick a track with -track <key>; list available presets with -list-tracks.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cedardb-demo/f1-telemetry/internal/db"
	"github.com/cedardb-demo/f1-telemetry/internal/sim"
)

func main() {
	totalLaps := flag.Int("laps", 50, "total race laps")
	tickHz := flag.Int("hz", 10, "telemetry sample rate")
	trackKey := flag.String("track", "cedar-park",
		`track preset key — see -list-tracks for the full set, or "random"`)
	listTracks := flag.Bool("list-tracks", false, "print the available track presets and exit")
	resetSchema := flag.Bool("reset-schema", false,
		"drop & recreate all tables on startup — destroys all data from prior sessions")
	flag.Parse()

	if *listTracks {
		printTracks()
		return
	}

	chosen := *trackKey
	if strings.EqualFold(strings.TrimSpace(chosen), "random") {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		chosen = sim.Presets[rng.Intn(len(sim.Presets))].Key
		log.Printf("track=random  picked=%s", chosen)
	}

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	log.Printf("connected to CedarDB")

	// Schema bootstrap. schema.sql is idempotent (every statement is
	// CREATE … IF NOT EXISTS), so we always call ApplySchema on cold
	// start — no SchemaPresent gating. The -reset-schema flag layers
	// destructive DROPs on top via ResetSchema.
	//
	// SchemaPresent is still called for diagnostic logging so it's
	// obvious from stdout whether the tables were already there.
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
			log.Printf("schema-presence probe: sessions table already present")
		default:
			log.Printf("schema-presence probe: sessions table missing")
		}
		if err := db.ApplySchema(ctx, pool); err != nil {
			log.Fatalf("apply schema: %v", err)
		}
	}

	track := sim.GenerateTrack(chosen, 240)
	drivers := sim.DefaultGrid()

	s, err := sim.NewSimulator(ctx, pool, track, drivers, *totalLaps, *tickHz)
	if err != nil {
		log.Fatalf("setup: %v", err)
	}
	log.Printf("session_id=%d  track=%q  drivers=%d  laps=%d  hz=%d",
		s.SessionID, track.Name, len(drivers), *totalLaps, *tickHz)

	if err := s.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("run: %v", err)
	}
	log.Printf("session %d finished cleanly", s.SessionID)
}

func printTracks() {
	fmt.Println("Available track presets:")
	fmt.Println()
	for _, p := range sim.Presets {
		fmt.Printf("  %-13s  %s\n", p.Key, p.Display)
		fmt.Printf("  %-13s  %s\n", "", p.Tagline)
		fmt.Printf("  %-13s  %.0f m\n", "", p.LapMeters)
		fmt.Println()
	}
	fmt.Println(`Use:  -track <key>      (or "random" to pick one at random)`)
}
