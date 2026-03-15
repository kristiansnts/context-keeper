<h1 align="center">
  <br>
  context-keeper
  <br>
</h1>

<h4 align="center">Persistent memory layer for <a href="https://claude.com/claude-code" target="_blank">Claude Code</a> and <a href="https://githubnext.com/projects/copilot-cli" target="_blank">GitHub Copilot CLI</a>.</h4>

<p align="center">
  <a href="LICENSE">
    <img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License">
  </a>
  <a href="plugin/.claude-plugin/plugin.json">
    <img src="https://img.shields.io/badge/version-0.1.0-green.svg" alt="Version">
  </a>
  <a href="https://github.com/kristiansnts/context-keeper">
    <img src="https://img.shields.io/github/stars/kristiansnts/context-keeper?style=social" alt="Stars">
  </a>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> •
  <a href="#how-it-works">How It Works</a> •
  <a href="#mcp-tools">MCP Tools</a> •
  <a href="#memory-types">Memory Types</a> •
  <a href="#workspace-support">Workspace Support</a> •
  <a href="#slash-commands">Slash Commands</a> •
  <a href="#live-dashboard">Live Dashboard</a> •
  <a href="#development">Development</a> •
  <a href="#license">License</a>
</p>

<p align="center">
  context-keeper automatically saves your project's decisions, patterns, and hard-won knowledge — then injects the right context into every Claude Code or GitHub Copilot session and every prompt. No manual commands. No forgetting.
</p>

---

## Quick Start

context-keeper works with both **Claude Code** and **GitHub Copilot CLI**.

### Claude Code

```
/plugin marketplace add kristiansnts/context-keeper
/plugin install context-keeper
```

### GitHub Copilot CLI

```
/plugin marketplace add kristiansnts/context-keeper
/plugin install context-keeper@context-keeper
```

Restart your CLI after installing. Memory loads automatically on every session start.

> **Running both at the same time?** No conflict — they share the same SQLite DB and the dashboard runs on a single unified port (`7374`). Whichever CLI starts first owns the dashboard; the second detects it's already running and skips.

**Key Features:**

- 🧠 **Persistent Memory** — Decisions, patterns, gotchas, and conventions survive across sessions
- 📊 **Progressive Disclosure** — Compact index at session start, full details fetched on demand (~90% token savings)
- 🔁 **Pattern Solutions** — Save reusable step-by-step approaches; Claude checks them before repeated tasks
- 🏢 **Workspace Memory** — Shared memory across backend, frontend, and mobile in a monorepo
- 🎯 **Action-Aware Hints** — Detects bug fix vs implementation vs refactor and hints the right save type
- 📜 **Decision History** — Full evolution trail for every architectural decision
- 🚫 **Rejected Plans** — Captures abandoned approaches so Claude never re-proposes them
- 🗂️ **File Maps, API Catalog & Schema** — Cache where features live, endpoint specs, and DB field types to eliminate file re-exploration
- ⏱️ **Staleness Tagging** — Every entry carries a `staleness_risk` level (`low`/`medium`/`high`) auto-defaulted by type
- 📈 **Session Telemetry** — Tracks exploration ratio, steps before first edit, and prompt memory hits per session
- 🖥️ **Live Dashboard** — Real-time memory feed at `http://localhost:7374` with stats bar and 12-tab UI
- ⚡ **Go-powered** — Zero Node.js runtime dependency; single pre-compiled binary per platform
- 🤖 **Fully Automatic** — Saves and retrieves without being asked

---

## How It Works

**5 Lifecycle Hooks:**

1. **`SessionStart`** — Injects compact memory index + workspace memory as system message; auto-starts dashboard subprocess if not already running
2. **`UserPromptSubmit`** — Searches relevant memory per prompt; detects action type and hints Claude; tracks memory injection hit count
3. **`PostToolUse`** — Captures `Read`/`Glob`/`Grep`/`LS` as exploration observations and `Edit`/`Write` as edit observations for session telemetry
4. **`Stop`** — Auto-saves session summary including `exploration_ratio`, `steps_before_first_edit`, `files_explored`, and prompt hit count
5. **`ExitPlanMode`** — Reminds Claude to save rejected plans as `rejected` entries

**Session Flow:**

```
Claude Code starts
      │
      ▼
SessionStart hook
      ├── Project memory: compact index injected into system message
      └── Workspace memory: shared cross-project context (if monorepo)

User sends a prompt
      │
      ▼
UserPromptSubmit hook
      ├── Detects: bug fix / implementation / refactor / repeated task
      ├── Injects type hint → Claude saves with correct type automatically
      └── Searches local + workspace DB → injects top 4 relevant entries

Claude works
      ├── remember()       → saves decision / pattern / gotcha / rejected / workspace
      ├── patterns()       → checks saved solutions before repeated tasks
      ├── search()         → finds relevant prior context
      └── get([ids])       → fetches full content for specific entries

Session ends
      │
      ▼
Stop hook → auto-saves session summary of all entries created this session
```

---

## MCP Tools

context-keeper provides **9 MCP tools** following a token-efficient **3-layer progressive disclosure** pattern:

**The 3-Layer Workflow:**

1. **Session start** — Compact index auto-injected (~1 line/entry, minimal tokens)
2. **`context`** — Refresh the compact index mid-session on demand
3. **`get`** — Fetch full content ONLY for specific IDs you actually need

**Available Tools:**

| Tool | Description |
|------|-------------|
| `remember` | Save any memory entry — see action guide below for which type to use |
| `update_decision` | Update an existing decision — old version preserved as history |
| `search` | Full-text search with `scope`: `local` / `workspace` / `global` / `all` |
| `context` | Get compact index of all saved entries |
| `get` | Fetch full content for specific entry IDs |
| `patterns` | List all solution patterns — call this before repeated tasks |
| `decisions` | List all architectural decisions |
| `conventions` | List all coding conventions |
| `history` | Show the full change history of a decision |

**Example workflow:**

```
// Session starts — compact index already injected:
// [decision] Use SQLite for storage — SQLite + FTS5 chosen for...
// [pattern]  Add new API endpoint — 1. Create route file, 2. Register in...
// [workspace] Auth token shape — { token, expiresAt, userId }

// User asks to add a new endpoint → UserPromptSubmit detects implementation task:
// [context-keeper: implementation task → save as "pattern" if reusable]

// Claude checks patterns first:
patterns(search="api endpoint")

// Claude fetches full steps:
get(ids=[4])

// Claude saves the approach for next time:
remember(type="pattern", title="Add API endpoint", content="1. Create route...\n2. Register in...")
```

---

## Memory Types

**Action → Type guide** (Claude follows this automatically via `UserPromptSubmit` hints):

| What you're doing | Type to save |
|---|---|
| Fix a bug / error / crash | `gotcha` — save what caused it + what fixed it |
| Implement / add / build something | `pattern` if reusable steps, `decision` if architectural |
| Refactor / restructure / migrate | `decision` |
| Try an approach and abandon it | `rejected` — save what was tried + why + what was chosen instead |
| Knowledge shared across all projects | `workspace` — API shapes, auth flows, env conventions |
| Project-specific coding convention | `convention` |
| Background context about the project | `context` |
| Which file owns a feature | `file-map` — e.g. "auth logic lives in `auth_service.go`"; local-only |
| API endpoint details | `api-catalog` — method, path, auth flag, handler; mirrored to workspace DB |
| DB table / field definition | `schema` — types, constraints, gotchas; mirrored to workspace DB |

All types are stored in `.context/context.db` (SQLite + FTS5, gitignored).

**Staleness tagging** — every entry carries a `staleness_risk` level auto-defaulted by type:
- `high` — `api-catalog`, `schema`, `file-map` (change frequently)
- `medium` — `decision`, `pattern`
- `low` — `gotcha`, `convention`, `context`, `note`, `rejected`, `workspace`

Pass `staleness_risk` explicitly to `remember()` to override the default.

---

## Workspace Support

For monorepos where you work on **backend, frontend, and mobile simultaneously**:

**Auto-detection** — context-keeper walks up from the project root looking for:
- `lerna.json`, `pnpm-workspace.yaml`, `nx.json`, `turbo.json`
- `package.json` with `"workspaces"` field
- `.context-keeper-workspace` sentinel file (drop this at your monorepo root for explicit control)

**Shared workspace DB** at `{monorepo-root}/.context/workspace.db` — automatically receives:
- All `decision` entries (architectural choices that affect multiple projects)
- All `pattern` entries (reusable solutions visible to all projects)
- All `workspace` entries (API contracts, shared types, auth flows)
- All `rejected` entries (prevents the same wrong approach from being proposed in any project)

**Cross-project search:**

```
// Search only workspace memory (shared across all projects)
search(query="auth token", scope="workspace")

// Search everywhere
search(query="validation pattern", scope="all")
```

**At session start**, workspace memory is injected as a separate section so Claude knows about cross-project context before writing a single line of code.

---

## Plan Mode Integration

When you exit plan mode after **rejecting a plan**, context-keeper prompts Claude to save it:

```
[context-keeper: Plan mode exited.
- If REJECTED → remember(type="rejected", title="[Plan] ...", content="what was proposed + why rejected + what you chose instead")
- If ACCEPTED → implementation will be tracked automatically]
```

Rejected plans accumulate over time — Claude stops re-proposing approaches you've already ruled out.

---

## Slash Commands

| Command | Description |
|---------|-------------|
| `/ck-context` | Reload full project memory into the current conversation |
| `/decisions` | View all architectural decisions with rationale |
| `/history [title or id]` | Show how a decision evolved — what changed and why |
| `/import-session [N\|all] [LIMIT]` | Scan past Claude Code and Copilot CLI session history and import discovered decisions, patterns, gotchas, file-maps, API catalog, and schema entries |

**`/import-session` options:**
- `/import-session` — import last 5 sessions per source for the current project
- `/import-session 10` — import last 10 sessions per source
- `/import-session all` — scan all projects in session history
- `/import-session all 3` — all projects, last 3 sessions each
- Deduplicates automatically (calls `search()` before saving); caps at 20 new entries per project

---

## Live Dashboard

Open **http://localhost:7374** while Claude works to watch memory being saved in real time. The dashboard auto-starts when a session begins — no manual launch needed.

**Stats bar** — five live metrics at the top, auto-refreshing every 30s:
- Entries saved · Memory hits · Est. tokens saved · Avg exploration ratio · Sessions tracked

| Tab | Shows |
|---|---|
| Live Feed | New entries as they're saved, animated in real-time via SSE |
| Decisions | All architectural decisions |
| Conventions | All coding conventions |
| Gotchas | Known pitfalls (including auto-captured failures) |
| Patterns | Saved solution patterns |
| Rejected | Abandoned approaches and plans |
| Workspace | Cross-project shared memory |
| Sessions | Auto-saved session summaries with telemetry |
| File Map | Feature-to-file mappings |
| API Catalog | Endpoint registry |
| Schema | DB table and field definitions |
| All Memory | Everything |

The dashboard also includes a **project dropdown** to filter memory across all projects tracked in the global DB (`~/.context-keeper/global.db`).

---

## System Requirements

- **Claude Code**: Latest version with plugin support
- No Node.js runtime required — the plugin ships pre-compiled Go binaries for:
  - macOS (arm64, amd64)
  - Linux (arm64, amd64)
  - Windows (amd64)

---

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTEXT_KEEPER_ROOT` | Project root — local DB at `$ROOT/.context/context.db` | `cwd` |
| `CONTEXT_KEEPER_PORT` | Dashboard port | `7374` |

Workspace DB is auto-detected at `{monorepo-root}/.context/workspace.db`. No configuration needed.

Global DB lives at `~/.context-keeper/global.db` — all entries from all projects, for `scope="global"` search.

---

## Development

```bash
git clone https://github.com/kristiansnts/context-keeper
cd context-keeper/go
go build ./cmd/context-keeper/
```

**Rebuild all platform binaries:**

```bash
cd go
GOOS=darwin  GOARCH=arm64 go build -o ../plugin/server/bin/context-keeper-darwin-arm64  ./cmd/context-keeper/
GOOS=darwin  GOARCH=amd64 go build -o ../plugin/server/bin/context-keeper-darwin-amd64  ./cmd/context-keeper/
GOOS=linux   GOARCH=amd64 go build -o ../plugin/server/bin/context-keeper-linux-amd64   ./cmd/context-keeper/
GOOS=linux   GOARCH=arm64 go build -o ../plugin/server/bin/context-keeper-linux-arm64   ./cmd/context-keeper/
GOOS=windows GOARCH=amd64 go build -o ../plugin/server/bin/context-keeper-windows-amd64.exe ./cmd/context-keeper/
```

**Project structure:**

```
plugin/                          # Installable plugin (shipped to users)
├── .claude-plugin/plugin.json   # Plugin metadata
├── .mcp.json                    # MCP config with ${CLAUDE_PLUGIN_ROOT} vars
├── hooks/
│   ├── hooks.json               # Claude Code hook definitions (5 hooks)
│   └── copilot-hooks.json       # GitHub Copilot CLI hook config
├── server/
│   ├── start.js                 # Node launcher — picks correct binary per platform
│   └── bin/                     # Pre-compiled Go binaries (all platforms)
└── skills/                      # Slash command skills
    ├── ck-context/SKILL.md
    ├── decisions/SKILL.md
    ├── history/SKILL.md
    └── import-session/SKILL.md

go/                              # Go source
├── cmd/context-keeper/main.go   # Entry point (MCP server + hook runner)
└── internal/
    ├── dashboard/               # HTTP dashboard server + SSE + embedded HTML
    ├── hooks/                   # Hook handlers (session-start, stop, etc.)
    ├── markdown/                # Context injection formatting
    ├── mcp/                     # MCP tool definitions
    └── storage/                 # SQLite storage, FTS5, multi-DB, migrations

.claude-plugin/marketplace.json  # Marketplace registration
```

---

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes, rebuild binaries
4. Submit a Pull Request

---

## License

[MIT](LICENSE) — use it anywhere, including commercially.

---

## Support

- **Issues**: [GitHub Issues](https://github.com/kristiansnts/context-keeper/issues)
- **Repository**: [github.com/kristiansnts/context-keeper](https://github.com/kristiansnts/context-keeper)

---

<p align="center">Built for developers who are tired of their AI having amnesia.</p>
