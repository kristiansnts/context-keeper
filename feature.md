# v0.1.0 Feature Proposals

## Research Data

Two real sessions were fully analysed. All numbers below come from parsing `events.jsonl` directly (not LLM-estimated).

### Session A — weworship full-stack integration (`context_keeper_analysis_v2.json`)

| Metric | Value |
|--------|-------|
| Session duration | 15 days |
| Total tool calls | 2,832 |
| Exploration calls | 705 (28%) |
| Implementation calls | 1,838 (65%) |
| Steps before first edit | **3** |
| Rediscovery events | **8** |
| "Still" rework turns | **13** / 210 user messages |

**Rediscovery events and token cost:**

| # | File | Views | Avoidable | Tokens/view | Wasted |
|---|------|-------|-----------|-------------|--------|
| RD-1 | `song/[id].tsx` | 41 | 20 | 1,350 | **27,000** |
| RD-2 | `router.go` | 16 | 8 | 600 | 4,800 |
| RD-3 | `song_service.go` | 19 | 10 | 900 | 9,000 |
| RD-4 | expo-file-system API | 9 | 7 | 200 | 1,400 |
| RD-5 | RN mutation error | 6 | 6 | 300 | 1,800 |
| RD-6 | `ThemeContext.tsx` | 4 | 3 | 400 | 1,200 |
| RD-7 | Song domain schema | 4 | 3 | 450 | 1,350 |
| RD-8 | `auth_service.go` | 10 | 6 | 600 | 3,600 |
| **Total** | | | | | **~50,000 tokens** |

**9 proposed entries would save ~34,100 tokens per session** (load cost: ~2,700 tokens → **12.6x ROI**).

> **Important correction from v1 analysis:** An earlier Haiku-generated estimate reported `exploration_ratio = 0.66` and `steps_before_first_edit = 45`. Both are wrong. Actual values from `events.jsonl`: **0.28** and **3**. The session is implementation-heavy (65%), not exploration-heavy. Waste comes from **rediscovery** (re-reading files across contexts), not from up-front exploration.

---

### Session B — abdiku full-stack + context-keeper pivot (`session-research-report.md`)

| Metric | Value |
|--------|-------|
| Session duration | 8 days, 15 checkpoints |
| Total tool calls | 2,206 |
| Exploration calls | 612 (27.8%) |
| Steps before first edit | 102 (sub-agent explores phase) |
| Rediscovery events | **7** |
| VEC cycles (view-edit-view) | **13** files |

**Errors from missing context → extra tool calls:**

| Error | Type | Extra calls |
|-------|------|-------------|
| TS mock `undefined` TS2345 | pattern | 8 |
| AuthContext timeout offline race | gotcha | 15 |
| Docker group requires runner restart | gotcha | 5 |
| Postgres constraint whitespace SQLSTATE[42704] | gotcha | 4 |
| GitHub Actions bash-only syntax | gotcha | 6 |
| storage.ts duplicate declaration corruption | gotcha | 3 |

**Total wasted**: ~3,000 tokens. **Net saving** with 10 entries: ~2,110 tokens (3.37x ROI).

**Additional entry types identified from Session B** (beyond the 3 primary types):

| Type | Wasted calls | ROI |
|------|-------------|-----|
| `file-map` | ~40 | 4x |
| `deploy-runbook` | ~12 | 3.3x |
| `env-catalog` | ~10 | 3.3x |
| `infra-map` | ~10 | 3.3x |
| `test-patterns` | ~8 | — |

---

## Context

**The problem is not exploration ratio — it's rediscovery.**

Both sessions show ~28% exploration ratio, already near the theoretical minimum. The waste comes from the same files being re-read 4–41 times across independent contexts because the assistant has no cache of what it already learned.

The three types of cached knowledge with the highest rediscovery cost:
- **Schema definitions** — Song domain type re-read 10 times in 10 different contexts (RD-7); router.go re-read 16 times (RD-2) → `api-catalog`
- **Component architecture** — `song/[id].tsx` read 41 times, 20 avoidable (RD-1) → `file-map`
- **API contracts** — cache key logic re-read 19 times (RD-3) → `schema` + `pattern`

Goal: measure and reduce **rediscovery events per session**. The `exploration_ratio` metric (Change 3) is a proxy — a high ratio means lots of reads without edits, which often correlates with re-reading known files. Baseline: ~28%. Target: no specific number, but each session summary should show trending down as entries accumulate.

**Key implementation note:** The JSONL observation pipeline is **already built** (`postToolUse` → `session-obs.jsonl` → `stop` → `AddSessionSummary`). The gap is that Read/Grep/Glob are silently skipped (`isNoisyTool()` → `return nil`), so `exploration_ratio` cannot be computed. Fix: write lightweight "explore" observations for these tools instead of discarding them.

CHANGELOG.md already updated (Unreleased v0.1.0 section added). This plan covers the Go code changes.

---

## Work Breakdown

### Change 1 — Three New Memory Types

**Files:** `storage.go`, `mcp/tools.go`, `dashboard/dashboard.go`, `hooks/hooks.go`

**Types to add:**

| Type | Description | Mirror to workspace? | Research evidence |
|------|-------------|---------------------|-------------------|
| `file-map` | "which file owns feature X" — file paths are project-specific | No | RD-1: song/[id].tsx re-read 41× (27K tokens); RD-2: router.go 16× (4.8K tokens) |
| `api-catalog` | endpoint registry: method, path, auth, handler | Yes | router.go viewed across 12 distinct contexts — highest context-switching cost after song/[id].tsx |
| `schema` | API response shapes, DB types, field definitions | Yes | RD-3: song_service.go 19×; RD-7: song domain type 10×; together ~10.5K tokens wasted |

`go/internal/storage/storage.go` lines 16–24:
```go
var ValidTypes = []string{
    "decision", "convention", "gotcha", "context",
    "note", "session", "rejected", "pattern", "workspace",
    "file-map", "api-catalog", "schema",  // NEW
}
var WorkspaceMirrorTypes = map[string]bool{
    "decision": true, "pattern": true, "workspace": true, "rejected": true,
    "api-catalog": true, "schema": true,  // NEW (file-map stays local)
}
```

`go/internal/mcp/tools.go` — extend `remember` type enum to include the 3 new values.

`go/internal/dashboard/dashboard.go` — in the embedded HTML JS block:
```javascript
// Add after 'sessions' in TABS array and TYPE_MAP object:
const TABS = ['live','decisions','conventions','gotchas','patterns','rejected',
               'workspace','sessions','file-map','api-catalog','schema','all'];
const TYPE_MAP = { ..., 'file-map':'file-map', 'api-catalog':'api-catalog', 'schema':'schema', all:'' };
```

`go/internal/hooks/hooks.go` — `sessionStart` empty-memory guide:
```
- map feature to files → type: "file-map"
- document an API endpoint → type: "api-catalog"
- document a DB table/field → type: "schema"
```
`detectTypeHint`: add keyword match → when prompt contains "endpoint", "route", "handler", "api" hint `api-catalog`; "table", "schema", "column", "migration" hint `schema`.

---

### Change 2 — Staleness Tagging

**Files:** `storage/schema.go`, `storage/storage.go`, `mcp/tools.go`

**New DB columns:**
```sql
ALTER TABLE memory ADD COLUMN staleness_risk TEXT NOT NULL DEFAULT 'low'
ALTER TABLE memory ADD COLUMN last_verified_at TEXT NOT NULL DEFAULT (datetime('now'))
```

Add both to the **lazy migration map** in `schema.go` (existing pattern: check `PRAGMA table_info`, apply if column absent). No version tracking needed — consistent with how `status`, `supersedes_id`, `change_reason` were added.

`Entry` struct additions:
```go
StalenessRisk   string  // "low" | "medium" | "high"
LastVerifiedAt  string  // datetime string
```

**Auto-default by type** in `insertEntry()` when `StalenessRisk == ""`:
```go
switch e.Type {
case "api-catalog", "schema", "file-map":
    e.StalenessRisk = "high"
case "decision", "convention", "pattern", "workspace":
    e.StalenessRisk = "medium"
default: // gotcha, note, context, session, rejected
    e.StalenessRisk = "low"
}
```

`mcp/tools.go` — add optional `staleness_risk` property to `remember` tool:
```json
"staleness_risk": {
    "type": "string",
    "enum": ["low", "medium", "high"],
    "description": "How quickly this entry may become stale. api-catalog/schema → high (auto-set if omitted); gotcha/pattern → low"
}
```

---

### Change 3 — Session Telemetry (exploration_ratio)

**Research note:** Baseline `exploration_ratio` across both sessions is ~28% (not 85% as previously estimated). The metric is still valuable for tracking per-session trends as context accumulates — the expectation is it trends down as saved entries reduce re-reads. The absolute number matters less than the direction over time.

**File:** `go/internal/hooks/hooks.go`

**Add `Kind` field to `rawObservation`:**
```go
type rawObservation struct {
    Tool string `json:"tool"`
    File string `json:"file,omitempty"`
    Cmd  string `json:"cmd,omitempty"`
    Exit *int   `json:"exit,omitempty"`
    Kind string `json:"kind,omitempty"` // "explore" | "edit" | "run"
}
```

**Replace the `isNoisyTool()` early-return** in `postToolUse` with selective capture:
```go
// Before: if isNoisyTool(toolName) { return nil }

// After:
switch toolName {
case "Read", "Glob", "Grep", "LS":
    var inp struct {
        FilePath string `json:"file_path"`
        Pattern  string `json:"pattern"`
        Path     string `json:"path"`
    }
    _ = json.Unmarshal(input.ToolInput, &inp)
    target := inp.FilePath
    if target == "" { target = inp.Pattern }
    if target == "" { target = inp.Path }
    _ = appendObservation(cfg, rawObservation{Tool: toolName, File: target, Kind: "explore"})
    return nil
case "WebFetch", "WebSearch", "Agent", "TodoRead", "TodoWrite":
    return nil // still skip — not project exploration
}
```

Also mark edit observations with `Kind: "edit"` and bash successes with `Kind: "run"`:
```go
// In Edit/Write/NotebookEdit path:
_ = appendObservation(cfg, rawObservation{Tool: toolName, File: filePath, Kind: "edit"})

// In Bash success path:
_ = appendObservation(cfg, rawObservation{Tool: "Bash", Cmd: cmd, Exit: &exit0, Kind: "run"})
```

**Update `buildSessionSummaryFromObs`** to compute telemetry after the existing files/commands section:
```go
exploreCount, totalCount := 0, len(obs)
stepsBeforeFirstEdit := -1
var exploredFiles []string
exploredSeen := map[string]bool{}

for i, o := range obs {
    if o.Kind == "explore" {
        exploreCount++
        if o.File != "" {
            base := filepath.Base(o.File)
            if !exploredSeen[base] {
                exploredSeen[base] = true
                exploredFiles = append(exploredFiles, base)
            }
        }
    }
    if stepsBeforeFirstEdit == -1 && o.Kind == "edit" {
        stepsBeforeFirstEdit = i
    }
}

if totalCount > 0 {
    ratio := float64(exploreCount) / float64(totalCount) * 100
    fmt.Fprintf(&sb, "Exploration ratio: %.0f%% (%d/%d)\n", ratio, exploreCount, totalCount)
    if stepsBeforeFirstEdit >= 0 {
        fmt.Fprintf(&sb, "Steps before first edit: %d\n", stepsBeforeFirstEdit)
    }
    if len(exploredFiles) > 0 {
        shown := exploredFiles
        if len(shown) > 8 { shown = shown[:8] }
        fmt.Fprintf(&sb, "Files explored (%d): %s\n", len(exploredFiles), strings.Join(shown, ", "))
    }
}
```

Session summary will include:
```
Files changed (3): auth_service.go, team_service.go, index.tsx
Commands run (2): go build ./..., npm test
Files explored (11): team_service.go, auth_service.go, router.go, ...
Exploration ratio: 31% (11/35)
Steps before first edit: 4
```

> Baseline from research: ~28% exploration ratio; 3–102 steps before first edit depending on session type. A ratio above ~45% in a single session is a signal worth investigating.

---

### Change 4 — Rebuild Binaries

```bash
cd go
GOOS=darwin  GOARCH=arm64 go build -o ../plugin/server/bin/context-keeper-darwin-arm64  ./cmd/context-keeper/
GOOS=darwin  GOARCH=amd64 go build -o ../plugin/server/bin/context-keeper-darwin-amd64  ./cmd/context-keeper/
GOOS=linux   GOARCH=amd64 go build -o ../plugin/server/bin/context-keeper-linux-amd64   ./cmd/context-keeper/
GOOS=linux   GOARCH=arm64 go build -o ../plugin/server/bin/context-keeper-linux-arm64   ./cmd/context-keeper/
GOOS=windows GOARCH=amd64 go build -o ../plugin/server/bin/context-keeper-windows-amd64.exe ./cmd/context-keeper/
```

---

### Change 5 — Dashboard Hit/Miss Stats Widget

**Goal:** Show users a panel that quantifies context-keeper's value — how many times it served memory (hits), how much exploration was avoided, and estimated tokens saved. Makes the ROI tangible.

**What "hit" means:** Every time `userPrompt` finds and injects relevant entries = 1 hit. Every exploration event in session JSONL that wasn't preceded by a memory hit = a miss (could have been cached).

**Tracking hits — `hooks/hooks.go`:**
The `userPrompt` function already fires memory injection. Add a counter write to a temp file:
```go
// In userPrompt(), after successful injection (sb.Len() > 0):
_ = appendPromptHit(cfg)  // increments .context/prompt-hits.tmp counter
```

New helper:
```go
func appendPromptHit(cfg storage.Config) error {
    path := filepath.Join(cfg.ProjectRoot, ".context", "prompt-hits.tmp")
    f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil { return err }
    defer f.Close()
    _, err = fmt.Fprintln(f, "1")
    return err
}
```

The `stop` hook reads and counts lines in `prompt-hits.tmp`, includes in session summary content, then deletes the file. Session entry content will have a line like:
```
Prompt hits: 8 (memory injected 8 times this session)
```

**New `/api/stats` endpoint — `dashboard/dashboard.go`:**
```go
// Aggregate from all session entries (type="session"):
type StatsResponse struct {
    TotalEntries    int     `json:"total_entries"`
    TotalSessions   int     `json:"total_sessions"`
    TotalHits       int     `json:"total_hits"`       // sum of "Prompt hits: N" from session entries
    AvgExploreRatio float64 `json:"avg_explore_ratio"` // avg from "Exploration ratio: X%" lines
    EstTokensSaved  int     `json:"est_tokens_saved"`  // total_hits * 600 (avg tokens per avoided re-exploration)
}
```

Parse from session entry content using simple string scanning (no regex needed — lines are formatted consistently by Stop hook).

**Dashboard Stats panel — HTML/JS:**
Add a stats bar at the top of the dashboard (above tabs):
```html
<div id="stats-bar">
  <div class="stat"><span id="stat-entries">—</span><label>entries saved</label></div>
  <div class="stat"><span id="stat-hits">—</span><label>memory hits</label></div>
  <div class="stat"><span id="stat-tokens">—</span><label>est. tokens saved</label></div>
  <div class="stat"><span id="stat-ratio">—</span><label>avg explore ratio</label></div>
  <div class="stat"><span id="stat-sessions">—</span><label>sessions tracked</label></div>
</div>
```

Fetched once on page load from `/api/stats`, auto-refreshes every 30s. Numbers animate in with a counter effect (CSS/JS).

Tooltip on "est. tokens saved": "Estimated based on 600 tokens per avoided re-exploration. Research baseline: ~28% exploration ratio; rediscovery events cost 1,400–27,000 tokens each depending on file size and context count."

**Files changed:** `hooks/hooks.go` (appendPromptHit, stop reads it), `dashboard/dashboard.go` (apiStats handler + HTML stats bar)

---

## Critical Files

| File | Change |
|------|--------|
| `go/internal/storage/storage.go` | New types in ValidTypes/WorkspaceMirrorTypes, Entry struct staleness fields, insertEntry auto-default, scan queries |
| `go/internal/storage/schema.go` | 2 new entries in migration map (`staleness_risk`, `last_verified_at`) |
| `go/internal/mcp/tools.go` | Extend type enum (×3), add `staleness_risk` param to `remember` |
| `go/internal/dashboard/dashboard.go` | 3 new tabs, `/api/stats` endpoint, stats bar HTML/JS |
| `go/internal/hooks/hooks.go` | `Kind` field on rawObservation, explore obs capture, telemetry in buildSessionSummaryFromObs, `appendPromptHit`, sessionStart guide + detectTypeHint |

---

---

## Future Entry Types (v0.2.0 candidates)

From Session B (abdiku), additional high-ROI types were identified but are not in scope for v0.1.0:

| Type | Description | Wasted calls (Session B) |
|------|-------------|--------------------------|
| `deploy-runbook` | Step-by-step deploy process for this project | ~12 |
| `env-catalog` | Required env vars per project + where to find values | ~10 |
| `infra-map` | VPS layout, Docker services, ports, domains | ~10 |
| `test-patterns` | How to write tests for this specific codebase | ~8 |
| `integration-point` | Cross-project data flows, service dependencies (Session A) | ~15 |

These are noted here so they can be added in a follow-up with minimal schema changes (same lazy-migration pattern).

---

## Verification

1. `cd go && go build ./cmd/context-keeper/` — must compile clean
2. `remember(type="api-catalog", title="GET /songs", content="GET /api/songs?chordpro=true for mobile", staleness_risk="high")` — entry saved, appears in api-catalog dashboard tab
3. `remember(type="file-map", title="Mobile route layout", content="app/(tabs)/ bottom nav, app/song/[id].tsx detail")` — saves as local-only (not in workspace DB)
4. `remember(type="schema", title="Song domain type", content="BESong: {id, title, artist, external_links (JSON string)}")` — appears in workspace DB (mirrored)
5. After a real session: Stop hook session summary should include `Exploration ratio: X%` and `Steps before first edit: N`
6. Old DBs (missing new columns): starting on an existing DB should auto-migrate via lazy ALTER TABLE with no error
7. Confirm `api-catalog` and `schema` entries appear in workspace DB; `file-map` stays local-only
8. `http://localhost:7374` shows stats bar with entries, hits, tokens saved, avg exploration ratio
9. Stats update after each session without manual refresh
