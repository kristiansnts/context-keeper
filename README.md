<h1 align="center">
  <br>
  context-keeper
  <br>
</h1>

<h4 align="center">Persistent memory layer built for <a href="https://claude.com/claude-code" target="_blank">Claude Code</a>.</h4>

<p align="center">
  <a href="LICENSE">
    <img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License">
  </a>
  <a href="package.json">
    <img src="https://img.shields.io/badge/version-0.0.1-green.svg" alt="Version">
  </a>
  <a href="package.json">
    <img src="https://img.shields.io/badge/node-%3E%3D18.0.0-brightgreen.svg" alt="Node">
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
  context-keeper automatically saves your project's decisions, patterns, and hard-won knowledge — then injects the right context into every Claude Code session and every prompt. No manual commands. No forgetting.
</p>

---

## Quick Start

Start a new Claude Code session and run:

```
/plugin marketplace add kristiansnts/context-keeper

/plugin install context-keeper
```

Restart Claude Code. Memory loads automatically on every session start.

**Key Features:**

- 🧠 **Persistent Memory** — Decisions, patterns, gotchas, and conventions survive across sessions
- 📊 **Progressive Disclosure** — Compact index at session start, full details fetched on demand (~90% token savings)
- 🔁 **Pattern Solutions** — Save reusable step-by-step approaches; Claude checks them before repeated tasks
- 🏢 **Workspace Memory** — Shared memory across backend, frontend, and mobile in a monorepo
- 🎯 **Action-Aware Hints** — Detects bug fix vs implementation vs refactor and hints the right save type
- 📜 **Decision History** — Full evolution trail for every architectural decision
- 🚫 **Rejected Plans** — Captures abandoned approaches so Claude never re-proposes them
- 🖥️ **Live Dashboard** — Real-time memory feed at `http://localhost:7373`
- 🤖 **Fully Automatic** — Saves and retrieves without being asked

---

## How It Works

**5 Lifecycle Hooks:**

1. **`SessionStart`** — Injects compact memory index + workspace memory as system message
2. **`UserPromptSubmit`** — Searches relevant memory per prompt; detects action type and hints Claude
3. **`PostToolUse`** — Auto-captures Bash failures as `gotcha` entries
4. **`Stop`** — Auto-saves session summary of everything learned
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

All types are stored in `.context/context.db` (SQLite + FTS5, gitignored).

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

---

## Live Dashboard

Open **http://localhost:7373** while Claude works to watch memory being saved in real time.

| Tab | Shows |
|---|---|
| Live Feed | New entries as they're saved, animated |
| Decisions | All architectural decisions |
| Conventions | All coding conventions |
| Gotchas | Known pitfalls (including auto-captured failures) |
| Patterns | Saved solution patterns |
| Rejected | Abandoned approaches and plans |
| Workspace | Cross-project shared memory |
| Sessions | Auto-saved session summaries |
| All Memory | Everything |

---

## System Requirements

- **Node.js**: 18.0.0 or higher
- **Claude Code**: Latest version with plugin support

---

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTEXT_KEEPER_ROOT` | Project root — local DB at `$ROOT/.context/context.db` | `process.cwd()` |
| `CONTEXT_KEEPER_PORT` | Dashboard port | `7373` |

Workspace DB is auto-detected at `{monorepo-root}/.context/workspace.db`. No configuration needed.

Global DB lives at `~/.context-keeper/global.db` — all entries from all projects, for `scope="global"` search.

---

## Development

```bash
git clone https://github.com/kristiansnts/context-keeper
cd context-keeper
npm install
npm run build        # Compiles TypeScript → plugin/server/index.js
```

**Local dev MCP config** — create `.mcp.json` at repo root (gitignored):

```json
{
  "mcpServers": {
    "context-keeper": {
      "command": "node",
      "args": ["plugin/server/index.js"],
      "env": {
        "CONTEXT_KEEPER_ROOT": "/your/absolute/project/path",
        "CONTEXT_KEEPER_PORT": "7373"
      }
    }
  }
}
```

**Project structure:**

```
plugin/                          # Installable plugin (shipped to users)
├── .claude-plugin/plugin.json   # Plugin metadata
├── .mcp.json                    # MCP config with ${CLAUDE_PLUGIN_ROOT} vars
├── hooks/hooks.json             # All 5 hook definitions
├── server/index.js              # Compiled MCP server + dashboard
└── skills/                      # Slash command skills
    ├── ck-context/SKILL.md
    ├── decisions/SKILL.md
    └── history/SKILL.md

packages/                        # Source (TypeScript)
├── core/src/                    # Storage layer (SQLite + FTS5, multi-DB support)
└── mcp-server/src/              # MCP server + all hook handlers

.claude-plugin/marketplace.json  # Marketplace registration
```

---

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes and run `npm run build`
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
