# Changelog

All notable changes to context-keeper are documented here.

---

## [Unreleased] — v0.1.0

### Added
- **Auto-capture tool observations** — `PostToolUse: *` writes all meaningful tool calls to `.context/session-obs.jsonl`; noise filtered at `Stop` hook; session summary includes "Files changed", "Commands run", "Bash failures"
- **3 new memory types**:
  - `file-map` — maps features to the files that own them (e.g. "auth logic lives in `auth_service.go` + `auth_handler.go`")
  - `api-catalog` — endpoint registry with method, path, auth flag, handler (e.g. `DELETE /teams/:id/members → requires admin`)
  - `schema` — DB table/field definitions and types (e.g. "`playlist_teams.members` is a JSON string, not an array")
- **Staleness tagging** — `staleness_risk: low|medium|high` field on entries + `last_verified_at` timestamp; Claude warned to distrust `high`-risk entries older than N commits
- **Session telemetry** — `Stop` hook records `exploration_ratio` (read tool calls / total tool calls), `steps_before_first_edit`, `rediscovery_count` to measure context-keeper's effectiveness over time

### Why
Research session data showed **84.6% of tool calls were exploration** (grep/read to answer "does X exist?"). These are cacheable. The three new types (`file-map`, `api-catalog`, `schema`) directly address the most common rediscovery patterns. Target: reduce `exploration_ratio` from ~85% to ~30%.

---

## [0.0.3] — 2026-03-15

### Added
- **Go binary rewrite** — replaced Node.js/TypeScript MCP server with pre-compiled Go binaries (darwin arm64/amd64, linux arm64/amd64, windows amd64); zero Node.js runtime required
- **Web dashboard** completely redesigned to match the original TypeScript dashboard style
  - Tab-based navigation: Live Feed, Decisions, Conventions, Gotchas, Patterns, Rejected, Workspace, Sessions, All Memory
  - **Live Feed tab** with SSE (Server-Sent Events) — entries appear in real-time as Claude saves them, no refresh needed
  - Pulsing green dot connection status indicator, auto-reconnects on disconnect
  - **Project dropdown** — filter memory by project using the global DB's source field (All Projects, or per-project)
  - Search bar to filter entries within the current tab
  - **"★ Star on GitHub"** button in the header
- **GitHub Copilot CLI support** — install command: `/plugin install context-keeper@context-keeper`
- **Unified dashboard port** — if port is already bound by another CLI, the second instance detects it (EADDRINUSE) and skips starting a duplicate; both CLIs share the same SQLite DB; live feed polls every 5s for cross-process entries
- `OnAdd` hook in storage — lets the dashboard broadcast new entries over SSE without polling
- `ListProjects()` — returns distinct project paths from the global DB for the dropdown
- `ListGlobalByProject(source, type)` — filter global DB entries by project path

### Fixed
- `${PROJECT_ROOT}` env var not expanded by Claude Code plugin system → binary now detects unexpanded templates (starting with `${`) and falls back to `os.Getwd()`
  - Previously created a literal `${PROJECT_ROOT}/.context/context.db` directory on every session start
- FTS5 search returning no results for multi-word queries — `sanitizeQuery` now builds `word1* OR word2* OR word3*` instead of implicit AND; single-word queries also use prefix matching

### Changed
- Dashboard port default changed from `7373` → `7374` to avoid conflict with Copilot CLI's old TypeScript server
- README updated: Go binary architecture, both CLI install instructions, updated system requirements (no Node.js needed)

---

## [0.0.2] — 2026-03-15

### Added
- **Claude Code plugin** (`plugin/`) — installable via `/plugin marketplace install context-keeper`
  - `plugin.json` and `.mcp.json` plugin metadata
  - **Hooks**: `SessionStart`, `Stop`, `UserPromptSubmit`, `PostToolUse` (Bash), `ExitPlanMode`
  - **Skills**: `/ck-context` (load all project memory), `/decisions` (view architectural decisions), `/history` (trace decision evolution)
- **MCP server** (TypeScript) with tools: `remember`, `search`, `context`, `get`, `decisions`, `conventions`, `patterns`, `update_decision`, `history`
- **Web dashboard** (TypeScript/Express) at `localhost:7373`
  - Tab-based UI with Live Feed, per-type tabs, All Memory
  - Real-time SSE feed for new entries
  - Entry cards with type badges, tags, metadata
- **Multi-DB architecture**: local project DB + workspace DB (monorepo) + global DB (`~/.context-keeper/global.db`)
  - Workspace auto-detection via `lerna.json`, `pnpm-workspace.yaml`, `nx.json`, `turbo.json`, `package.json#workspaces`, or `.context-keeper-workspace` sentinel
  - Mirroring rules: `decision`, `pattern`, `workspace`, `rejected` → workspace DB; all types → global DB
- **9 memory types** with strict action→type semantics: `decision`, `convention`, `gotcha`, `context`, `note`, `pattern`, `rejected`, `workspace`, `session`
- `update_decision` tool — preserves old version as history, creates new active entry linked via `supersedes_id`
- **FTS5 full-text search** with triggers for auto-indexing on insert/update/delete
- `UserPromptSubmit` hook detects action keywords (fix, implement, refactor, etc.) and injects type hints
- `SessionStart` hook injects project memory summary into Claude's context
- `Stop` hook saves session summary to memory

### Changed
- Removed VS Code extension (`packages/vscode/`) — focused on Claude Code plugin
- Removed CLI package (`packages/cli/`) — `ctx` CLI removed in favor of MCP tools
- Expanded `packages/core` storage engine with multi-DB support, FTS5, decision history, workspace detection

### Architecture
- TypeScript monorepo: `packages/core` (storage) + `packages/mcp-server` (MCP + HTTP server)
- Runtime: Node.js + `better-sqlite3`

---

## [0.0.1] — 2026-03-14

### Added
- **Initial monorepo** with four packages:
  - `packages/core` — SQLite storage engine with FTS5 full-text search, CRUD for memory entries
  - `packages/cli` — `ctx` CLI: `init`, `add`, `search`, `list`, `summary`, `delete` commands
  - `packages/mcp-server` — MCP server compatible with Claude Code, Cursor, Continue.dev
  - `packages/vscode` — VS Code extension with Copilot `@ctx` chat participant
- **Memory entry schema**: `id`, `type`, `title`, `content`, `tags`, `source`, `created_at`, `updated_at`
- **Memory types**: `decision`, `convention`, `gotcha`, `context`, `note`, `session`
- Zero Python dependency — SQLite only
- Git-native team sharing via `.context/` folder
- `npx`-ready setup

### Architecture
- TypeScript monorepo with shared `packages/core` storage
- Runtime: Node.js + `better-sqlite3`
- SQLite database at `{project}/.context/context.db`

---

## Roadmap

- `v0.1.0` — Auto-observation capture + 3 new types (`file-map`, `api-catalog`, `schema`) + staleness tagging + session telemetry (next)
- `v0.2.0` — Stable release with docs site
- Semantic search (embeddings) as opt-in
- Web UI for manual memory editing
