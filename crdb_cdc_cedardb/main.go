package main

// $Id: main.go,v 1.5 2025/06/17 15:22:16 mgoddard Exp mgoddard $

/*

  CREATE CHANGEFEED FOR TABLE public.osm_names
  INTO 'webhook-https://localhost:8443/cdc/geohash3,city,name?insecure_tls_skip_verify=true'
  WITH updated;

*/

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChangeEvent represents one element of the "payload" array
type ChangeEvent struct {
	Topic string                 `json:"topic"`
	Key   []interface{}          `json:"key"`
	After map[string]interface{} `json:"after"`
}

// WebhookRequest is the envelope sent by CockroachDB changefeeds
type WebhookRequest struct {
	Payload []ChangeEvent `json:"payload"`
}

var pool *pgxpool.Pool

func main() {
	// Load configuration
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		log.Fatal("PG_DSN must be set")
	}
	certStr := os.Getenv("TLS_CERT")
	keyStr := os.Getenv("TLS_KEY")
	if certStr == "" || keyStr == "" {
		log.Fatal("TLS_CERT and TLS_KEY must be set")
	}
	fmt.Println("PG_DSN: ", dsn)

	// Initialize pgx pool
	var err error
	pool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/cdc/", webhookHandler)

	// Load TLS cert
  certPem := []byte(certStr)
  keyPem := []byte(keyStr)
	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		log.Fatalf("Failed to load TLS cert/key: %v", err)
	}

	srv := &http.Server{
		Addr:    ":8443",
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	}

	log.Println("Starting HTTPS webhook on :8443")
	log.Fatal(srv.ListenAndServeTLS("", ""))
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// /cdc/col1,col2,...
	path := strings.TrimPrefix(r.URL.Path, "/cdc/")
	if path == "" {
		http.Error(w, "missing primary-key columns", http.StatusBadRequest)
		return
	}
	pkCols := strings.Split(path, ",")

	var req WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	for _, ev := range req.Payload {
		tbl := ev.Topic
		if len(ev.Key) != len(pkCols) {
			// mismatch PK count; skip
			continue
		}

		ctx := context.Background()
		tx, err := pool.Begin(ctx)
		if err != nil {
			http.Error(w, "failed to begin tx", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		if ev.After == nil {
			// DELETE
			where := make([]string, len(pkCols))
			args := make([]interface{}, len(pkCols))
			for i, col := range pkCols {
				where[i] = fmt.Sprintf(`"%s" = $%d`, col, i+1)
				args[i] = ev.Key[i]
			}
			sql := fmt.Sprintf(`DELETE FROM "%s" WHERE %s;`, tbl, strings.Join(where, " AND "))
			if _, err := tx.Exec(ctx, sql, args...); err != nil {
				http.Error(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
				return
			}
		} else {
			// UPSERT
			cols := make([]string, 0, len(ev.After))
			ph := make([]string, 0, len(ev.After))
			args := make([]interface{}, 0, len(ev.After))
			ups := make([]string, 0, len(ev.After))
			idx := 1

			for col, val := range ev.After {
				cols = append(cols, fmt.Sprintf(`"%s"`, col))
				ph = append(ph, fmt.Sprintf("$%d", idx))
				args = append(args, val)

				// only non-PK columns go in SET
				isPK := false
				for _, pk := range pkCols {
					if pk == col {
						isPK = true
						break
					}
				}
				if !isPK {
					ups = append(ups, fmt.Sprintf(`"%s" = EXCLUDED."%s"`, col, col))
				}
				idx++
			}
			sql := fmt.Sprintf(
				`INSERT INTO "%s" (%s) VALUES (%s)
                 ON CONFLICT (%s) DO UPDATE SET %s;`,
				tbl,
				strings.Join(cols, ","),
				strings.Join(ph, ","),
				`"`+strings.Join(pkCols, `","`)+`"`,
				strings.Join(ups, ","),
			)
			for nRetry := 0; nRetry < 3; nRetry++ {
				if _, err := tx.Exec(ctx, sql, args...); err != nil {
					if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "40001" {
						// We can retry a 40001 error
						tx.Rollback(ctx)
						sleepMs := int((int(math.Pow(float64(2), float64(nRetry))) * 100) + rand.Intn(100-1) + 1)
						time.Sleep(time.Duration(sleepMs) * time.Millisecond)
						continue
					}
				} else {
					break
				}
				http.Error(w, fmt.Sprintf("upsert error: %v", err), http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, fmt.Sprintf("commit error: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
