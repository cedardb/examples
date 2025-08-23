package main

/*

  CREATE CHANGEFEED FOR TABLE public.customer
  INTO 'webhook-https://host.docker.internal:8443/cdc/c_w_id,c_d_id,c_id?insecure_tls_skip_verify=true'
  WITH updated;

*/

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
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

// ========================= STATS: rolling per-table metrics =========================

const (
	statsHorizonSeconds = 3600 // keep 1 hour of 1-second buckets
)

type bucket struct {
	tsSec int64 // UNIX seconds of the bucket
	nReq  int64 // number of requests that contained this table during that second
	nByte int64 // total request JSON bytes attributed to this table during that second
}

type series struct {
	mu      sync.RWMutex
	buckets []bucket // length = statsHorizonSeconds
}

func newSeries() *series {
	return &series{buckets: make([]bucket, statsHorizonSeconds)}
}

func (s *series) add(nowSec int64, bytes int64) {
	idx := int(nowSec % statsHorizonSeconds)
	s.mu.Lock()
	defer s.mu.Unlock()
	b := &s.buckets[idx]
	if b.tsSec != nowSec {
		*b = bucket{tsSec: nowSec, nReq: 0, nByte: 0}
	}
	b.nReq++
	b.nByte += bytes
}

type statsStore struct {
	mu   sync.RWMutex
	data map[string]*series // key = table name
}

func newStatsStore() *statsStore {
	return &statsStore{data: make(map[string]*series)}
}

func (ss *statsStore) ensure(tbl string) *series {
	ss.mu.RLock()
	ser := ss.data[tbl]
	ss.mu.RUnlock()
	if ser != nil {
		return ser
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ser = ss.data[tbl]; ser == nil {
		ser = newSeries()
		ss.data[tbl] = ser
	}
	return ser
}

func (ss *statsStore) snapshot(nowSec int64) []tblRow {
	// compute metrics for each table over 60s, 300s, 900s, 3600s
	windows := []int64{60, 300, 900, 3600}
	type accum struct{ cnt, bytes int64 }
	results := []tblRow{}

	ss.mu.RLock()
	defer ss.mu.RUnlock()
	for tbl, ser := range ss.data {
		acc := make([]accum, len(windows))
		ser.mu.RLock()
		for i := range ser.buckets {
			b := ser.buckets[i]
			if b.tsSec == 0 {
				continue
			}
			age := nowSec - b.tsSec
			if age < 0 || age >= statsHorizonSeconds {
				continue
			}
			for wi, w := range windows {
				if age < w {
					acc[wi].cnt += b.nReq
					acc[wi].bytes += b.nByte
				}
			}
		}
		ser.mu.RUnlock()

		row := tblRow{
			Table:     tbl,
			Rate1m:    rate(acc[0].cnt, windows[0]),
			Rate5m:    rate(acc[1].cnt, windows[1]),
			Rate15m:   rate(acc[2].cnt, windows[2]),
			Rate1h:    rate(acc[3].cnt, windows[3]),
			AvgLen1m:  avgLen(acc[0].bytes, acc[0].cnt),
			AvgLen5m:  avgLen(acc[1].bytes, acc[1].cnt),
			AvgLen15m: avgLen(acc[2].bytes, acc[2].cnt),
			AvgLen1h:  avgLen(acc[3].bytes, acc[3].cnt),
		}
		results = append(results, row)
	}
	return results
}

func rate(cnt int64, windowSec int64) float64 {
	if windowSec <= 0 {
		return 0
	}
	return float64(cnt) / float64(windowSec)
}

func avgLen(bytes int64, cnt int64) float64 {
	if cnt == 0 {
		return 0
	}
	return float64(bytes) / float64(cnt)
}

var globalStats = newStatsStore()

// ========================= end STATS ==============================================

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
	mux.HandleFunc("/stats", statsHandler) // NEW: HTML stats endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/stats", http.StatusFound)
	})

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

	// Read the raw body so we can measure JSON size consistently
	raw, err := readAllLimit(r, 50<<20) // 50MB safety limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req WebhookRequest
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Collect unique tables present in this request (count once per tbl per request)
	tblsInThisRequest := make(map[string]struct{})

	for _, ev := range req.Payload {
		tbl := ev.Topic
		tblsInThisRequest[tbl] = struct{}{} // for stats

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
						// retry 40001
						tx.Rollback(ctx)
						sleepMs := int((int(math.Pow(2, float64(nRetry))) * 100) + rand.Intn(100-1) + 1)
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

	// Record stats once per table present in this request
	nowSec := time.Now().Unix()
	jsonLen := int64(len(raw))
	for tbl := range tblsInThisRequest {
		ser := globalStats.ensure(tbl)
		ser.add(nowSec, jsonLen)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func readAllLimit(r *http.Request, limit int64) ([]byte, error) {
	if r.Body == nil {
		return nil, fmt.Errorf("empty body")
	}
	defer r.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(http.MaxBytesReader(nil, r.Body, limit)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ========================= /stats HTML handler =====================================

type tblRow struct {
	Table                   string
	Rate1m, Rate5m, Rate15m float64
	Rate1h                  float64
	AvgLen1m, AvgLen5m      float64
	AvgLen15m, AvgLen1h     float64
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	nowSec := time.Now().Unix()
	rows := globalStats.snapshot(nowSec)
	// Sort alphabetically by table name
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Table < rows[j].Table
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := statsTpl.Execute(w, rows); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

var statsTpl = template.Must(template.New("stats").Funcs(template.FuncMap{
	"f3":  func(v float64) string { return fmt.Sprintf("%.3f", v) },
	"esc": html.EscapeString,
}).Parse(`
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Changefeed Stats</title>
<!-- Auto-refresh every second -->
<meta http-equiv="refresh" content="1">
<style>
  :root { --row-even: #ffffff; --row-odd: #f7f7f9; }
  body { font-family: system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif; margin: 24px; }
  h1 { margin-bottom: 8px; }
  table { border-collapse: collapse; width: 100%; }
  th, td { padding: 8px 10px; border-bottom: 1px solid #eee; text-align: right; }
  th:first-child, td:first-child { text-align: left; }
  thead th { position: sticky; top: 0; background: #fafafa; z-index: 1; }
  tbody tr:nth-child(odd)  { background: var(--row-odd); }
  tbody tr:nth-child(even) { background: var(--row-even); }
  tbody tr:hover { filter: brightness(0.98); }
  .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  .sub { color: #666; font-size: 12px; margin-bottom: 10px; }
</style>
</head>
<body>
  <h1>Changefeed Stats</h1>
  <div class="sub">Averages computed over rolling windows using 1-second buckets. Rates are <b>requests per second</b> for requests that contained the table. Page auto-refreshes every second.</div>
  <table>
    <thead>
      <tr>
        <th>Table</th>
        <th>Req/s 1m</th>
        <th>Req/s 5m</th>
        <th>Req/s 15m</th>
        <th>Req/s 1h</th>
        <th>Avg JSON bytes 1m</th>
        <th>Avg JSON bytes 5m</th>
        <th>Avg JSON bytes 15m</th>
        <th>Avg JSON bytes 1h</th>
      </tr>
    </thead>
    <tbody>
      {{range .}}
      <tr>
        <td class="mono">{{esc .Table}}</td>
        <td>{{f3 .Rate1m}}</td>
        <td>{{f3 .Rate5m}}</td>
        <td>{{f3 .Rate15m}}</td>
        <td>{{f3 .Rate1h}}</td>
        <td>{{f3 .AvgLen1m}}</td>
        <td>{{f3 .AvgLen5m}}</td>
        <td>{{f3 .AvgLen15m}}</td>
        <td>{{f3 .AvgLen1h}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>
`))
