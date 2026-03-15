package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/context-keeper/context-keeper/internal/storage"
)

// Start launches the dashboard HTTP server in the background.
// If the port is already bound (another CLI has the dashboard), it skips
// starting a second server — both CLIs share the same SQLite DB so the
// existing dashboard already shows all memory.
func Start(store *storage.Storage, port string) {
	if port == "" {
		port = "7373"
	}

	addr := "127.0.0.1:" + port

	// Try to claim the port before starting the full server.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			// Another CLI instance already owns the dashboard — that's fine.
			fmt.Fprintf(os.Stderr, "[context-keeper] dashboard already running at http://localhost:%s\n", port)
			return
		}
		fmt.Fprintf(os.Stderr, "[context-keeper] dashboard: %v\n", err)
		return
	}

	hub := &sseHub{clients: make(map[chan []byte]struct{})}

	// Hook into storage so live feed gets new entries via SSE.
	store.OnAdd(func(e storage.Entry) {
		b, _ := json.Marshal(e)
		hub.broadcast(b)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/entries", apiEntries(store))
	mux.HandleFunc("/api/projects", apiProjects(store))
	mux.HandleFunc("/api/stats", apiStats(store))
	mux.HandleFunc("/events", sseHandler(hub))
	mux.HandleFunc("/", serveUI)

	go func() {
		if err := http.Serve(ln, mux); err != nil {
			fmt.Fprintf(os.Stderr, "[context-keeper] dashboard: %v\n", err)
		}
	}()
}

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var syscallErr *os.SyscallError
		if errors.As(opErr.Err, &syscallErr) {
			return errors.Is(syscallErr.Err, syscall.EADDRINUSE)
		}
	}
	return false
}

// ── SSE hub ───────────────────────────────────────────────────────────────────

type sseHub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func (h *sseHub) subscribe() chan []byte {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *sseHub) broadcast(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

// ── handlers ──────────────────────────────────────────────────────────────────

func sseHandler(hub *sseHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprint(w, "retry: 3000\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		ch := hub.subscribe()
		defer hub.unsubscribe(ch)

		for {
			select {
			case data, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "event: memory\ndata: %s\n\n", data)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			case <-r.Context().Done():
				return
			}
		}
	}
}

func apiEntries(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typ := r.URL.Query().Get("type")
		project := r.URL.Query().Get("project")
		var (
			entries []storage.Entry
			err     error
		)
		if project != "" {
			src := project
			if project == "all" {
				src = ""
			}
			entries, err = store.ListGlobalByProject(src, typ)
		} else {
			entries, err = store.ListAll(typ)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(entries)
	}
}

func apiProjects(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projects := store.ListProjects()
		// Return basenames for display but full paths as values
		type project struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		out := make([]project, 0, len(projects))
		for _, p := range projects {
			out = append(out, project{Path: p, Name: filepath.Base(p)})
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(out)
	}
}

type statsResponse struct {
	TotalEntries    int     `json:"total_entries"`
	TotalSessions   int     `json:"total_sessions"`
	TotalHits       int     `json:"total_hits"`
	AvgExploreRatio float64 `json:"avg_explore_ratio"`
	EstTokensSaved  int     `json:"est_tokens_saved"`
}

func apiStats(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := store.ListAll("all")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		stats := statsResponse{}
		stats.TotalEntries = len(entries)

		var ratioSum float64
		var ratioCount int

		for _, e := range entries {
			if e.Type != "session" {
				continue
			}
			stats.TotalSessions++
			for _, line := range strings.Split(e.Content, "\n") {
				if strings.HasPrefix(line, "Prompt hits: ") {
					parts := strings.Fields(line)
					if len(parts) >= 3 {
						if n, err := strconv.Atoi(parts[2]); err == nil {
							stats.TotalHits += n
						}
					}
				}
				if strings.HasPrefix(line, "Exploration ratio: ") {
					// "Exploration ratio: 85% (11/13)"
					rest := strings.TrimPrefix(line, "Exploration ratio: ")
					pct := strings.TrimSuffix(strings.Fields(rest)[0], "%")
					if f, err := strconv.ParseFloat(pct, 64); err == nil {
						ratioSum += f
						ratioCount++
					}
				}
			}
		}

		if ratioCount > 0 {
			stats.AvgExploreRatio = ratioSum / float64(ratioCount)
		}
		stats.EstTokensSaved = stats.TotalHits * 600

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(stats)
	}
}

func serveUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(uiHTML))
}

// ── HTML ──────────────────────────────────────────────────────────────────────

var uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>context-keeper</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0f0f0f; color: #e0e0e0; height: 100vh; display: flex; flex-direction: column; }

    /* Header */
    header { background: #1a1a1a; border-bottom: 1px solid #2a2a2a; padding: 10px 20px; display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
    header h1 { font-size: 15px; font-weight: 600; color: #fff; white-space: nowrap; }
    .header-right { display: flex; align-items: center; gap: 10px; margin-left: auto; flex-wrap: wrap; }
    .status { display: flex; align-items: center; gap: 6px; font-size: 12px; color: #888; }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: #4caf50; animation: pulse 2s infinite; flex-shrink: 0; }
    @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

    /* Project select */
    .project-select { background: #111; border: 1px solid #2a2a2a; border-radius: 6px; padding: 5px 24px 5px 10px; color: #ccc; font-size: 12px; outline: none; cursor: pointer; appearance: none; -webkit-appearance: none; background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10' viewBox='0 0 24 24' fill='none' stroke='%23666' stroke-width='2'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E"); background-repeat: no-repeat; background-position: right 7px center; transition: border-color 0.15s; }
    .project-select:hover { border-color: #3a3a3a; }
    .project-select option { background: #1a1a1a; }

    /* Search */
    .search-input { background: #111; border: 1px solid #2a2a2a; border-radius: 6px; padding: 5px 10px; color: #ccc; font-size: 12px; outline: none; width: 180px; transition: border-color 0.15s; }
    .search-input:focus { border-color: #4caf50; }
    .search-input::placeholder { color: #444; }

    /* Star button */
    .star-btn { background: transparent; border: 1px solid #2a2a2a; border-radius: 6px; padding: 5px 10px; color: #f0a050; font-size: 12px; text-decoration: none; white-space: nowrap; transition: all 0.15s; }
    .star-btn:hover { border-color: #f0a050; background: #2a1e0a; }

    /* Tabs */
    .tabs { background: #1a1a1a; border-bottom: 1px solid #2a2a2a; display: flex; gap: 0; padding: 0 20px; overflow-x: auto; flex-shrink: 0; }
    .tab { padding: 10px 14px; font-size: 13px; cursor: pointer; border-bottom: 2px solid transparent; color: #888; transition: all 0.15s; white-space: nowrap; user-select: none; }
    .tab.active { color: #fff; border-bottom-color: #4caf50; }
    .tab:hover:not(.active) { color: #ccc; }

    /* Main */
    main { flex: 1; overflow-y: auto; padding: 16px 20px; }
    .empty { color: #555; font-size: 13px; text-align: center; padding: 40px; }

    /* Entry cards */
    .entry { background: #1a1a1a; border: 1px solid #2a2a2a; border-radius: 8px; padding: 14px 16px; margin-bottom: 10px; }
    .entry.live-new { animation: fadeIn 0.4s ease; border-color: #2d4a2d; }
    @keyframes fadeIn { from { opacity: 0; transform: translateY(-6px); } to { opacity: 1; } }
    .entry-header { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; flex-wrap: wrap; }
    .badge { font-size: 10px; font-weight: 600; padding: 2px 8px; border-radius: 20px; text-transform: uppercase; letter-spacing: 0.05em; flex-shrink: 0; }
    .badge-decision   { background: #1e3a5f; color: #64b5f6; }
    .badge-convention { background: #1a3a1a; color: #81c784; }
    .badge-gotcha     { background: #3a2a0a; color: #ffb74d; }
    .badge-context    { background: #2a1a3a; color: #ce93d8; }
    .badge-note       { background: #2a2a2a; color: #9e9e9e; }
    .badge-rejected   { background: #3a1a1a; color: #ef5350; }
    .badge-session    { background: #1a2a3a; color: #4dd0e1; }
    .badge-pattern    { background: #1a3a2a; color: #69f0ae; }
    .badge-workspace  { background: #2a2a0a; color: #fff176; }
    .badge-file-map   { background: #0a2a3a; color: #80deea; }
    .badge-api-catalog { background: #1a0a3a; color: #b39ddb; }
    .badge-schema     { background: #2a1a0a; color: #ffcc80; }
    .entry-title { font-size: 14px; font-weight: 500; color: #fff; flex: 1; }
    .entry-id { font-size: 11px; color: #444; margin-left: auto; flex-shrink: 0; }
    .entry-supersedes { font-size: 11px; color: #f0a050; }
    .entry-content { font-size: 13px; color: #aaa; line-height: 1.5; white-space: pre-wrap; }
    .entry-change-reason { font-size: 12px; color: #f0a050; margin-top: 4px; }
    .entry-tags { display: flex; gap: 4px; margin-top: 6px; flex-wrap: wrap; }
    .tag { font-size: 11px; background: #222; color: #777; padding: 2px 6px; border-radius: 4px; }
    .entry-meta { font-size: 11px; color: #444; margin-top: 6px; }
    .source-badge { font-size: 11px; color: #4caf50; margin-left: 6px; }

    /* Stats bar */
    #stats-bar { background: #141414; border-bottom: 1px solid #2a2a2a; display: flex; gap: 0; padding: 8px 20px; flex-shrink: 0; overflow-x: auto; }
    .stat { display: flex; flex-direction: column; align-items: center; padding: 4px 20px; border-right: 1px solid #222; min-width: 100px; }
    .stat:last-child { border-right: none; }
    .stat span { font-size: 18px; font-weight: 700; color: #4caf50; line-height: 1.2; }
    .stat label { font-size: 10px; color: #555; text-transform: uppercase; letter-spacing: 0.08em; margin-top: 2px; white-space: nowrap; }

    /* Scrollbar */
    ::-webkit-scrollbar { width: 5px; height: 5px; }
    ::-webkit-scrollbar-track { background: transparent; }
    ::-webkit-scrollbar-thumb { background: #2a2a2a; border-radius: 3px; }
  </style>
</head>
<body>
  <header>
    <h1>🧠 context-keeper</h1>
    <div class="header-right">
      <select id="project-select" class="project-select" onchange="onProjectChange()">
        <option value="">Current project</option>
      </select>
      <input type="text" id="search" class="search-input" placeholder="Search…" oninput="onSearch()">
      <div class="status"><div class="dot" id="dot"></div><span id="status-text">Connecting…</span></div>
      <a class="star-btn" href="https://github.com/kristiansnts/context-keeper" target="_blank" rel="noopener">★ Star on GitHub</a>
    </div>
  </header>

  <div id="stats-bar">
    <div class="stat"><span id="stat-entries">—</span><label>entries saved</label></div>
    <div class="stat"><span id="stat-hits">—</span><label>memory hits</label></div>
    <div class="stat"><span id="stat-tokens">—</span><label>est. tokens saved</label></div>
    <div class="stat"><span id="stat-ratio">—</span><label>avg explore ratio</label></div>
    <div class="stat"><span id="stat-sessions">—</span><label>sessions tracked</label></div>
  </div>

  <div class="tabs">
    <div class="tab active" onclick="showTab('live')">Live Feed</div>
    <div class="tab" onclick="showTab('decisions')">Decisions</div>
    <div class="tab" onclick="showTab('conventions')">Conventions</div>
    <div class="tab" onclick="showTab('gotchas')">Gotchas</div>
    <div class="tab" onclick="showTab('patterns')">Patterns</div>
    <div class="tab" onclick="showTab('rejected')">Rejected</div>
    <div class="tab" onclick="showTab('workspace')">Workspace</div>
    <div class="tab" onclick="showTab('sessions')">Sessions</div>
    <div class="tab" onclick="showTab('file-map')">File Map</div>
    <div class="tab" onclick="showTab('api-catalog')">API Catalog</div>
    <div class="tab" onclick="showTab('schema')">Schema</div>
    <div class="tab" onclick="showTab('all')">All Memory</div>
  </div>

  <main id="main">
    <div id="tab-live"><div class="empty">Waiting for Claude to save memory…</div></div>
    <div id="tab-decisions" style="display:none"></div>
    <div id="tab-conventions" style="display:none"></div>
    <div id="tab-gotchas" style="display:none"></div>
    <div id="tab-patterns" style="display:none"></div>
    <div id="tab-rejected" style="display:none"></div>
    <div id="tab-workspace" style="display:none"></div>
    <div id="tab-sessions" style="display:none"></div>
    <div id="tab-file-map" style="display:none"></div>
    <div id="tab-api-catalog" style="display:none"></div>
    <div id="tab-schema" style="display:none"></div>
    <div id="tab-all" style="display:none"></div>
  </main>

<script>
const TABS = ['live','decisions','conventions','gotchas','patterns','rejected','workspace','sessions','file-map','api-catalog','schema','all'];
const TYPE_MAP = { decisions:'decision', conventions:'convention', gotchas:'gotcha', patterns:'pattern', rejected:'rejected', workspace:'workspace', sessions:'session', 'file-map':'file-map', 'api-catalog':'api-catalog', schema:'schema', all:'' };

let currentTab = 'live';
let currentProject = '';
let searchQuery = '';

// ── render ────────────────────────────────────────────────────────────────────
function badge(type) {
  return '<span class="badge badge-' + type + '">' + type + '</span>';
}

function esc(s) {
  return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

function renderEntry(e, isNew) {
  const date = e.UpdatedAt ? new Date(e.UpdatedAt).toLocaleString() : '';
  const tags = e.Tags && e.Tags.length
    ? '<div class="entry-tags">' + e.Tags.map(t => '<span class="tag">' + esc(t) + '</span>').join('') + '</div>' : '';
  const supersedes = e.SupersedesID
    ? '<span class="entry-supersedes">↻ Updated (was #' + e.SupersedesID + ')</span>' : '';
  const reason = e.ChangeReason
    ? '<div class="entry-change-reason">Why changed: ' + esc(e.ChangeReason) + '</div>' : '';
  const source = e.Source
    ? '<span class="source-badge">' + esc(e.Source.split('/').pop()) + '</span>' : '';
  return '<div class="entry' + (isNew ? ' live-new' : '') + '">' +
    '<div class="entry-header">' + badge(e.Type) + '<span class="entry-title">' + esc(e.Title) + '</span>' + supersedes + source + '<span class="entry-id">#' + e.ID + '</span></div>' +
    '<div class="entry-content">' + esc(e.Content) + '</div>' +
    reason + tags +
    '<div class="entry-meta">' + date + '</div>' +
    '</div>';
}

function matchesSearch(e, q) {
  if (!q) return true;
  q = q.toLowerCase();
  return (e.Title||'').toLowerCase().includes(q) ||
         (e.Content||'').toLowerCase().includes(q) ||
         (e.Tags||[]).some(t => t.toLowerCase().includes(q));
}

// ── tabs ──────────────────────────────────────────────────────────────────────
function showTab(tab) {
  document.querySelectorAll('.tab').forEach((t, i) => t.classList.toggle('active', TABS[i] === tab));
  document.querySelectorAll('main > div').forEach(d => d.style.display = 'none');
  document.getElementById('tab-' + tab).style.display = 'block';
  currentTab = tab;
  if (tab !== 'live') loadTab(tab);
}

async function loadTab(tab) {
  const type = TYPE_MAP[tab] ?? '';
  let url = '/api/entries' + (type ? '?type=' + type : '');
  if (currentProject) url += (type ? '&' : '?') + 'project=' + encodeURIComponent(currentProject);

  const el = document.getElementById('tab-' + tab);
  try {
    const res = await fetch(url);
    let entries = await res.json();
    if (searchQuery) entries = entries.filter(e => matchesSearch(e, searchQuery));
    if (!entries || !entries.length) { el.innerHTML = '<div class="empty">Nothing saved yet.</div>'; return; }
    el.innerHTML = entries.map(e => renderEntry(e, false)).join('');
  } catch (_) {
    el.innerHTML = '<div class="empty">Failed to load.</div>';
  }
}

// ── project & search ──────────────────────────────────────────────────────────
function onProjectChange() {
  currentProject = document.getElementById('project-select').value;
  // Re-seed seen IDs for new scope so existing entries don't flood live feed
  liveSeenIds = new Set();
  initLiveFeed();
  if (currentTab !== 'live') loadTab(currentTab);
}

function onSearch() {
  searchQuery = document.getElementById('search').value;
  if (currentTab !== 'live') loadTab(currentTab);
}

async function loadProjects() {
  try {
    const res = await fetch('/api/projects');
    const projects = await res.json();
    const sel = document.getElementById('project-select');
    while (sel.options.length > 1) sel.remove(1);
    if (projects && projects.length > 0) {
      const opt = document.createElement('option');
      opt.value = 'all'; opt.textContent = 'All Projects';
      sel.appendChild(opt);
      projects.forEach(p => {
        const o = document.createElement('option');
        o.value = p.path; o.textContent = p.name;
        sel.appendChild(o);
      });
    }
  } catch (_) {}
}

// ── SSE live feed + cross-process polling ─────────────────────────────────────
// SSE gives instant updates for entries saved by the process that owns the
// dashboard. Polling every 5s catches entries from other CLI processes (e.g.
// Copilot CLI) that share the same DB but don't own the dashboard port.
let liveSeenIds = new Set();

function connectSSE() {
  const es = new EventSource('/events');
  es.addEventListener('memory', ev => {
    const entry = JSON.parse(ev.data);
    liveSeenIds.add(entry.ID);
    prependLiveEntry(entry);
  });
  es.onopen = () => {
    document.getElementById('status-text').textContent = 'Connected';
    document.getElementById('dot').style.background = '#4caf50';
  };
  es.onerror = () => {
    document.getElementById('status-text').textContent = 'Disconnected';
    document.getElementById('dot').style.background = '#f44336';
    es.close();
    setTimeout(connectSSE, 5000);
  };
}

function prependLiveEntry(entry) {
  const liveTab = document.getElementById('tab-live');
  const empty = liveTab.querySelector('.empty');
  if (empty) empty.remove();
  liveTab.insertAdjacentHTML('afterbegin', renderEntry(entry, true));
}

// Poll for entries not yet seen in live feed (from other CLI processes).
async function pollLiveFeed() {
  let url = '/api/entries';
  if (currentProject) url += '?project=' + encodeURIComponent(currentProject);
  try {
    const res = await fetch(url);
    const entries = await res.json();
    if (!entries) return;
    // Prepend any new IDs in reverse order to maintain newest-first
    const unseen = entries.filter(e => !liveSeenIds.has(e.ID));
    unseen.reverse().forEach(e => {
      liveSeenIds.add(e.ID);
      prependLiveEntry(e);
    });
  } catch (_) {}
}

// Seed seen IDs on load so existing entries don't flood the live feed.
async function initLiveFeed() {
  let url = '/api/entries';
  if (currentProject) url += '?project=' + encodeURIComponent(currentProject);
  try {
    const res = await fetch(url);
    const entries = await res.json();
    if (entries) entries.forEach(e => liveSeenIds.add(e.ID));
  } catch (_) {}
}

setInterval(pollLiveFeed, 5000);

// ── stats ─────────────────────────────────────────────────────────────────────
function animateNumber(el, target, suffix) {
  const start = 0;
  const duration = 600;
  const startTime = performance.now();
  function step(now) {
    const progress = Math.min((now - startTime) / duration, 1);
    const val = Math.round(progress * target);
    el.textContent = val + (suffix || '');
    if (progress < 1) requestAnimationFrame(step);
  }
  requestAnimationFrame(step);
}

async function loadStats() {
  try {
    const res = await fetch('/api/stats');
    const s = await res.json();
    animateNumber(document.getElementById('stat-entries'), s.total_entries, '');
    animateNumber(document.getElementById('stat-hits'), s.total_hits, '');
    animateNumber(document.getElementById('stat-tokens'), s.est_tokens_saved, '');
    document.getElementById('stat-ratio').textContent = s.avg_explore_ratio ? s.avg_explore_ratio.toFixed(0) + '%' : '—';
    animateNumber(document.getElementById('stat-sessions'), s.total_sessions, '');
  } catch (_) {}
}

setInterval(loadStats, 30000);

// ── init ──────────────────────────────────────────────────────────────────────
loadProjects();
loadStats();
initLiveFeed().then(connectSSE);
</script>
</body>
</html>`
