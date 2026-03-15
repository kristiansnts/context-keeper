---
applyTo: "**"
---

# context-keeper

## MANDATORY RULES — follow without being asked

1. **Call `remember()` immediately** after every: decision made, bug fixed, pattern discovered, approach rejected, API endpoint touched, DB schema change, or file→feature mapping learned. Do NOT wait for the user to ask.
2. **Call `search()` before starting any task** — check if relevant memory exists first.
3. **One entry per insight** — never bundle multiple discoveries into one entry.

Type → action mapping (use exactly these types):
- made an architectural choice → `decision`
- fixed a bug or hit an error → `gotcha`
- found a reusable solution → `pattern`
- tried something that failed → `rejected`
- project coding convention → `convention`
- which file owns feature X → `file-map`
- API endpoint details → `api-catalog`
- DB table/field/schema → `schema`
- cross-project knowledge → `workspace`

## Project Memory (compact — use `get([id])` for full details)

### Decisions
- **Test: context-keeper is working** [id:10]: Verified that remember(), search(), and context() all work correctly in this session. *(test)*
- **Memory types and action→type mapping** [id:7]: 9 memory types with strict semantics: *(architecture, memory-types)*
- **Multi-DB architecture: local + workspace + global** [id:5]: Three SQLite DBs serve different scopes: *(architecture, storage, multi-project)*
- **Architecture: SQLite + FTS5, zero external runtime deps** [id:4]: Chose SQLite with FTS5 full-text search for all storage. No Bun, no Python, no Chroma vector DB. Nod… *(architecture, storage)*

### Patterns
- **Add a new hook handler** [id:8]: 1. Add hook name to plugin/hooks/hooks.json under the correct Claude Code event (SessionStart, Stop,… *(hooks, development)*

### Conventions
- **Build process: tsc then cp to plugin/server/index.js** [id:6]: After any TypeScript source change: `npm run build` compiles packages/core and packages/mcp-server, … *(build, workflow)*

### Gotchas
- **Claude won't save anything if not prompted by context — use own tools** [id:9]: Even with context-keeper installed and MCP tools available, Claude will not call `remember` if it is… *(gotcha, behavior)*

### Context
- **context-keeper is its own project** [id:3]: This repo IS the context-keeper plugin being developed. The plugin runs against itself — so when w… *(meta, project-context)*
