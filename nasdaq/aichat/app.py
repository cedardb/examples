import os, re, json, requests, time
from typing import Dict, Any
from fastapi import FastAPI
from fastapi.responses import HTMLResponse, StreamingResponse
import psycopg2
from psycopg2.extras import RealDictCursor

DB_HOST=os.environ.get("DB_HOST","localhost")
DB_NAME=os.environ.get("DB_NAME","postgres")
DB_USER=os.environ.get("DB_USER","postgres")
DB_PASSWORD=os.environ.get("DB_PASSWORD")
API_KEY=os.environ.get("OPENROUTER_API_KEY")
LLM_MODEL=os.environ.get("LLM_MODEL","anthropic/claude-sonnet-4.5")
BASE_URL = os.getenv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1/chat/completions")


app = FastAPI()

INDEX_HTML = r"""
<!doctype html>
<html><head><meta charset="utf-8"><title>CedarDB Chat</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
:root{--bg:#fafafa;--card:#fff;--text:#111;--muted:#666;--line:#e6e6e6;--acc:#2a7;--badge:#eef5ff}
*{box-sizing:border-box}
body{font-family:system-ui,Segoe UI,Arial,sans-serif;background:var(--bg);color:var(--text);max-width:1100px;margin:2rem auto;padding:0 1rem}
.row{display:flex;gap:.6rem}
.input{flex:1;min-height:42px;padding:.6rem .8rem;border-radius:10px;border:1px solid #ccc;background:#fff}
button{padding:.6rem .9rem;border-radius:10px;border:1px solid #ccc;background:#fff;cursor:pointer}
button:disabled{opacity:.5;cursor:not-allowed}
.mini{padding:.3rem .6rem;border-radius:8px;font-size:.9rem}
.grid{display:grid;grid-template-columns:1.6fr 1fr;gap:1rem}
.card{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:0}
.card-head{display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid var(--line);padding:.7rem 1rem}
.subcard{border-top:1px solid var(--line);padding:.7rem 1rem}
.subhead{display:flex;align-items:center;justify-content:space-between;margin-bottom:.4rem}
h3,h4{margin:0}
.scroll{max-height:55vh;overflow:auto}
.codeblock{margin:0;background:#f6f6f6;border:1px solid var(--line);border-radius:10px;padding:.8rem;white-space:pre-wrap;word-break:break-word}
.badge{background:var(--badge);border:1px solid #cfe3ff;border-radius:999px;padding:.2rem .6rem;font-size:.85rem}
.status{margin-bottom:1rem;display:none;padding:.5rem .75rem;border:1px dashed var(--line);border-radius:10px;background:#f9fff8}
.reasoning p{margin:.4rem 0}
.reasoning ul{margin:.2rem 0 .6rem 1.2rem}
.timeline{margin:0;padding:0;list-style:none}
.timeline li{display:flex;justify-content:space-between;gap:.5rem;border-bottom:1px dashed var(--line);padding:.25rem 0}
.timeline .name{font-weight:600}
.timeline .status{display: inline;color:var(--muted);white-space:nowrap}
table{border-collapse:collapse;width:100%}
th,td{border-bottom:1px solid var(--line);padding:.4rem .6rem;text-align:left}
thead th{position:sticky;top:0;background:#fff}
</style>
</head>
<body>
  <h2 style="margin-bottom:.5rem">CedarDB • Talk to Your Data</h2>

  <div class="toolbar row" style="align-items:center;margin-bottom:1rem">
    <input id="q" class="input" placeholder="Ask a question, e.g. 'Top 5 symbols by average traded volume in the last 5 minutes'"/>
    <button id="sendBtn" type="button">Run</button>
  </div>

  <div id="status" class="status"></div>

  <div class="grid">
    <!-- LEFT: Results -->
    <section class="card">
      <div class="card-head">
        <h3>Results</h3>
        <div class="badges">
          <span id="rowsBadge" class="badge">0 rows</span>
        </div>
      </div>
      <div id="results" class="scroll"></div>
    </section>

    <!-- RIGHT: Details -->
    <section class="card">
      <div class="card-head"><h3>Details</h3></div>

      <div class="subcard">
        <div class="subhead">
          <h4>SQL</h4>
          <div>
            <button id="copySqlBtn" class="mini" disabled>Copy</button>
                <button id="openGrafanaBtn" class="mini" disabled>Open in Grafana</button>
          </div>
        </div>
        <pre id="sqlBox" class="codeblock">(none)</pre>
      </div>

      <div class="subcard">
        <h4>Steps</h4>
        <div id="timeline"></div>
      </div>

      <div class="subcard">
        <details id="reasoningDetails" open>
          <summary><strong>Reasoning</strong></summary>
          <div id="reasoning" class="reasoning"></div>
        </details>
      </div>

    </section>
  </div>
</body>
<script>
const GRAFANA_BASE = `http://${location.hostname}:3000`;
const DS_UID = 'fdl2zuq913klcb';

const qEl = document.getElementById('q');
const sendBtn = document.getElementById('sendBtn');
const statusBox = document.getElementById('status');
const resultsEl = document.getElementById('results');
const rowsBadge = document.getElementById('rowsBadge');
const sqlBox = document.getElementById('sqlBox');
const copySqlBtn = document.getElementById('copySqlBtn');
const openGrafanaBtn = document.getElementById('openGrafanaBtn');
const reasoningEl = document.getElementById('reasoning');
const timelineEl = document.getElementById('timeline');

let lastSQL = null;
let steps = new Map(); // id -> {name, startTs, durationMs}
let timelineTick = null;


function updateTimelineTicker(){
  const hasPending = [...steps.values()].some(s => s.durationMs == null);
  if (hasPending && !timelineTick){
    timelineTick = setInterval(renderTimeline, 300);
  } else if (!hasPending && timelineTick){
    clearInterval(timelineTick);
    timelineTick = null;
  }
}

function setStatus(s){
  if (!s){ statusBox.style.display='none'; statusBox.textContent=''; return; }
  statusBox.style.display='block';
  statusBox.textContent = s;
}

function escapeHtml(s){
  return String(s).replace(/[&<>"']/g, m => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]));
}

function enableSqlActions(enabled){
  copySqlBtn.disabled = !enabled;
  openGrafanaBtn.disabled = !enabled;
}

function renderTable(rows){
  if (!rows || !rows.length){ resultsEl.innerHTML = '<div style="color:#777">No rows.</div>'; rowsBadge.textContent = '0 rows'; return; }
  const keys = Object.keys(rows[0]);
  let html = '<table><thead><tr>' + keys.map(k=>`<th>${escapeHtml(k)}</th>`).join('') + '</tr></thead><tbody>';
  for (const row of rows){
    html += '<tr>' + keys.map(k=>`<td>${escapeHtml(row[k])}</td>`).join('') + '</tr>';
  }
  html += '</tbody></table>';
  resultsEl.innerHTML = html;
  rowsBadge.textContent = `${rows.length} row${rows.length===1?'':'s'}`;
}

function renderReasoning({method}){
  reasoningEl.innerHTML = `
    ${method ? `<p><em>Method:</em> ${escapeHtml(method)}</p>` : ''}
  `;
}

function formatDuration(ms){
  if (ms == null) ms = 0;
  if (ms < 1000) return `${ms} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(2)} s`;
  const m = Math.floor(s / 60);
  const sec = Math.round(s % 60).toString().padStart(2,'0');
  return `${m}:${sec} min`;
}

function renderTimeline(){
  const ordered = [...steps.values()].sort((a,b)=>a.startTs - b.startTs);
  let html = '<ul class="timeline">';
  for (const s of ordered){
    const ms = (s.durationMs == null) ? (Date.now() - s.startTs) : s.durationMs;
    const dur = formatDuration(s.durationMs);
    html += `<li><span class="name">${escapeHtml(s.name)}</span><span class="status">${dur}</span></li>`;
  }
  html += '</ul>';
  timelineEl.innerHTML = html;
  updateTimelineTicker();
}

function startStep(id, name, ts){
  steps.set(id, {id, name, startTs: ts, durationMs: null});
  renderTimeline();
}
function endStep(id, ts, durationMs){
  const s = steps.get(id);
  s.durationMs = durationMs;
  renderTimeline();
}

async function handleSend(){
  const text = qEl.value.trim(); if (!text) return;

  // Reset panels
  setStatus('Sending...');
  rowsBadge.textContent = '0 rows';
  resultsEl.innerHTML = '';
  sqlBox.textContent = '(none)';
  reasoningEl.innerHTML = '';
  steps = new Map(); renderTimeline();
  lastSQL = null;
  enableSqlActions(false);

  const r = await fetch('/api/chat_stream', {
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body: JSON.stringify({question:text})
  });

  if (!r.ok || !r.body){ setStatus('Request failed.'); return; }

  const reader = r.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  while (true){
    const {value, done} = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, {stream:true});
    let idx;
    while ((idx = buffer.indexOf('\n')) >= 0){
      const line = buffer.slice(0, idx).trim();
      buffer = buffer.slice(idx+1);
      if (!line) continue;
      let msg; try { msg = JSON.parse(line); } catch { continue; }

      switch (msg.type){
        case 'step':
          startStep(msg.id, msg.name, msg.server_ts);
          setStatus(msg.name + '...');
          break;
        case 'step_end':
          endStep(msg.id, msg.server_ts, msg.duration_ms);
          setStatus(null);
          break;
        case 'sql':
          lastSQL = msg.sql || null;
          sqlBox.textContent = lastSQL || '(none)';
          enableSqlActions(!!lastSQL);
          break;
        case 'reasoning':
          renderReasoning(msg);
          break;
        case 'result':
          renderTable(msg.rows || []);
          break;
        case 'status':
          setStatus(msg.message);
          break;
        case 'error':
          setStatus('Error: ' + (msg.error || 'unknown'));
          break;
        case 'done':
          setStatus('Done.');
          setTimeout(()=>setStatus(null), 900);
          break;
      }
    }
  }
}

sendBtn.addEventListener('click', handleSend);
qEl.addEventListener('keydown', e => { if (e.key === 'Enter'){ e.preventDefault(); handleSend(); } });

copySqlBtn.addEventListener('click', async () => {
  if (!lastSQL) return;
  try{ await navigator.clipboard.writeText(lastSQL); copySqlBtn.textContent='Copied!'; setTimeout(()=>copySqlBtn.textContent='Copy',900); }
  catch{ copySqlBtn.textContent='Copy failed'; setTimeout(()=>copySqlBtn.textContent='Copy',1200); }
});

openGrafanaBtn.onclick = () => {
  if (!lastSQL) return;
  const state = {
    queries: [{
      refId: 'A',
      datasource: { type: 'postgres', uid: DS_UID },
      editorMode: 'code',
      format: 'table',
      rawSql: lastSQL
    }]
  };
  const url = `${GRAFANA_BASE}/explore?left=${encodeURIComponent(JSON.stringify(state))}`;
  window.open(url, '_blank');
};
</script>
</html>
"""

def get_conn():
  conn = psycopg2.connect(
    host=DB_HOST, dbname=DB_NAME, user=DB_USER, password=DB_PASSWORD,
    connect_timeout=5
  )
  conn.autocommit = True
  conn.readonly = True
  return conn

def introspect_schema(conn) -> str:
  with conn.cursor(cursor_factory=RealDictCursor) as cur:
    try:
      cur.execute("""
        SELECT table_schema, table_name, string_agg(column_name||' '||data_type, ', ' ORDER BY ordinal_position) AS cols
        FROM information_schema.columns
        WHERE table_schema NOT IN ('pg_catalog','information_schema')
        GROUP BY table_schema, table_name
        ORDER BY table_schema, table_name
      """)
      tables=cur.fetchall()
      lines=[]
      for t in tables:
          schema = t['table_schema']
          name = t['table_name']
          cols = t['cols']

          # Try to get 1–2 sample rows
          try:
              cur.execute(f'SELECT * FROM "{schema}"."{name}" LIMIT 2;')
              rows = cur.fetchall()
              sample_json = json.dumps(rows, default=str)
          except Exception as e:
              sample_json = f"<no sample: {e}>"
              conn.rollback()

          lines.append(f"{schema}.{name}({cols}) SAMPLE: {sample_json}")

      return "\n".join(lines)
    except Exception as e:
        conn.rollback()
        raise RuntimeError(f"Schema introspection failed: {e}")

READONLY_BLOCKLIST=re.compile(r"\\b(UPDATE|DELETE|INSERT|ALTER|DROP|TRUNCATE|CREATE|GRANT|REVOKE)\\b", re.I)
MAX_RETRIES = 2
ROW_LIMIT = 200

def enforce_select_only(sql_text: str) -> str:
    s = sql_text.strip().strip(";")
    # Only allow SELECT or WITH ... SELECT
    if not re.match(r"^(with\s+.+?select|select)\b", s, re.I | re.S):
        raise ValueError("Only SELECT queries are allowed.")
    if READONLY_BLOCKLIST.search(s):
        raise ValueError("Potentially unsafe SQL detected.")
    # Ensure a limit
    if not re.search(r"\blimit\s+\d+\b", s, re.I):
        s = f"{s} LIMIT {ROW_LIMIT}"
    return s

def call_llm(system: str, messages: list):
    payload = {"model": LLM_MODEL, "temperature": 0.2,
               "messages": [{"role":"system","content":system}] + messages}
    r = requests.post(
        BASE_URL,
        headers={"Authorization": f"Bearer {API_KEY}", "Content-Type":"application/json"},
        data=json.dumps(payload),
        timeout=60
    )
    if r.status_code >= 400:
        raise RuntimeError(f"OpenAI {r.status_code}: {r.text}")
    return r.json()


def llm_sql(question:str, schema:str, prev_error: str | None = None, prev_sql: str | None = None)->str:
  system = (
    "You are a SQL generator for Postgres-compatible CedarDB. "
    "Respond ONLY with a single JSON object and nothing else. "
    "JSON schema:\n"
      "{\n"
      '  "sql": "<ONE SELECT statement only>",\n'
      '  "method": "<2-4 sentences, high level>"\n'
      "}\n"
    "Add LIMIT 200 if no limit. The database is read-only."
    "The dataset consists of Level 3 order data."
    "When orders don't agree on price, all pending orders are kept in the order book until they are matched."
    "Whenever orders are matched, we get an execution event that shows the volume BUT USUALLY has NULL as price. The price has to be inferred from the referenced order."
    "Upated orders are inserted as new events that reference the original order id."
    "The timestamps are nanoseconds since midnight of the day the trades were recorded. It's a historic dataset and we don't have the actual date. This means you can't filter by date, only by time of day."
  )

  if prev_error:
    user = f"""SCHEMA:
    {schema}

    USER QUESTION:
    {question}

    PREVIOUS SQL THAT CAUSED ERROR:
    {prev_sql}

    DATABASE ERROR MESSAGE:
    {prev_error}

    Returned corrected SQL. Avoid the cause of the previous error."""
  else:
    user = f"""SCHEMA:
    {schema}

    USER QUESTION:
    {question}

    Return just the SQL."""

  res = call_llm(system, [{"role":"user","content":user}])
  content = res['choices'][0]['message']['content'].strip()

  # Be tolerant: extract the first JSON object
  try:
      first_brace = content.find('{')
      last_brace = content.rfind('}')
      obj = json.loads(content[first_brace:last_brace+1])
  except Exception as e:
      # Back-compat: if model still returned raw SQL, wrap it
      obj = {"sql": content, "method": ""}

  # Normalize keys
  obj.setdefault("method", "")
  # Strip code fences if a model ignored instructions
  if isinstance(obj.get("sql"), str):
      obj["sql"] = obj["sql"].replace("```sql","").replace("```","").strip().rstrip(";")

  return obj
def run_with_retries(conn, question: str, schema: str):
    """Generate SQL, optionally repair, then run."""
    last = None
    error_msg = None
    tries = 0
    while tries <= MAX_RETRIES:
        last = llm_sql(question, schema, error_msg, last["sql"] if isinstance(last, dict) and "sql" in last else None) if tries > 0 else llm_sql(question, schema)
        sql_safe = enforce_select_only(last["sql"])
        try:
            with conn.cursor(cursor_factory=RealDictCursor) as cur:
              # Execute in a safe, read-only, timed context
              cur.execute("SET statement_timeout = 8000")
              cur.execute(sql_safe)
              rows = cur.fetchall()
              conn.commit()
              reasoning = {
                "method": last.get("method", ""),
              }
              return sql_safe, rows, tries, reasoning
        except Exception as e:
            # Collect a concise error for the LLM
            conn.rollback()
            error_msg = str(e)
            tries += 1
    # If we get here, no luck
    raise RuntimeError(f"Failed after {MAX_RETRIES+1} attempts. Last error: {error_msg}")


@app.get("/", response_class=HTMLResponse)
def index():
  return INDEX_HTML

@app.post("/api/chat_stream")
def chat_stream(payload: Dict[str, Any]):
    q = (payload.get("question") or "").strip()

    def now_ms(): return int(time.time() * 1000)

    def emit(obj: Dict[str, Any]):
        # always include a server timestamp to make client-side timing robust
        obj.setdefault("server_ts", now_ms())
        return (json.dumps(obj, default=str) + "\n").encode("utf-8")

    def gen():
        if not q:
            yield emit({"type":"error","error":"Empty question"})
            return

        current_step = None
        step_counter = 0

        def start_step(name: str):
            nonlocal current_step, step_counter
            # End any previous step implicitly
            if current_step is not None:
                end_step()
            step_counter += 1
            current_step = {
                "id": step_counter,
                "name": name,
                "t0": now_ms()
            }
            yield emit({"type":"step","id": current_step["id"], "name": name})

        def end_step():
            nonlocal current_step
            if current_step is None:
                return
            t1 = now_ms()
            dur = t1 - current_step["t0"]
            payload = {
                "type":"step_end",
                "id": current_step["id"],
                "name": current_step["name"],
                "duration_ms": dur
            }
            yield emit(payload)
            current_step = None

        # Step 1: introspect
        yield from start_step("Introspecting schema")
        try:
            with get_conn() as conn:
                schema = introspect_schema(conn)
                yield from end_step()

                # Step 2: LLM (attempt N)
                last = None
                error_msg = None
                tries = 0
                while tries <= MAX_RETRIES:
                    yield from start_step(f"Calling LLM (attempt {tries+1})")
                    last = llm_sql(q, schema, error_msg, last["sql"] if isinstance(last, dict) and "sql" in last else None) if tries > 0 else llm_sql(q, schema)
                    sql_text = last["sql"]

                    # Validate SQL
                    try:
                        sql_safe = enforce_select_only(sql_text)
                    except Exception as e:
                        error_msg = f"Validation: {e}"
                        # end LLM attempt
                        yield from end_step()
                        # Start a short repair phase to make the timeline explicit
                        yield from start_step("Repairing SQL")
                        # no blocking work here; this step just marks the state
                        yield from end_step()
                        tries += 1
                        continue

                    # LLM attempt succeeded
                    yield emit({
                        "type":"sql",
                        "sql": sql_safe,
                        "attempt": tries + 1
                    })
                    yield emit({
                        "type":"reasoning",
                        "method": last.get("method", ""),
                    })
                    yield from end_step()

                    # Step 3: run query
                    yield from start_step("Running query")
                    try:
                        with conn.cursor(cursor_factory=RealDictCursor) as cur:
                            cur.execute("SET statement_timeout = 8000")
                            cur.execute(sql_safe)
                            rows = cur.fetchall()
                            yield emit({
                                "type":"result",
                                "rows_count": len(rows),
                                "rows": rows[:ROW_LIMIT]
                            })
                            yield from end_step()
                            yield emit({"type":"done"})
                            return
                    except Exception as e:
                        yield from end_step()
                        conn.rollback()
                        error_msg = str(e)
                        tries += 1
                        # Loop will try another LLM attempt

                # failed after retries
                yield emit({"type":"error","error":f"Failed after {MAX_RETRIES+1} attempts. Last error: {error_msg}"})

        except Exception as e:
            # end any open step with error
            yield from end_step()
            yield emit({"type":"error","error":str(e)})

    return StreamingResponse(gen(), media_type="application/x-ndjson")
