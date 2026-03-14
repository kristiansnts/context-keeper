# context-keeper 🧠

> **Your AI coding assistant has amnesia. Fix it.**

`context-keeper` is a universal memory layer for AI coding tools. It persists your project's architectural decisions, coding conventions, and hard-won knowledge — and injects it into **every AI tool you use**.

Works with **GitHub Copilot**, **Claude Code**, **Cursor**, **Continue.dev**, **Windsurf**, and any [MCP](https://modelcontextprotocol.io)-compatible client.

---

## The Problem

You spend 20 minutes explaining your auth architecture to Claude Code. Tomorrow, it has no idea what you talked about. Next week, Copilot suggests the exact pattern you decided against 3 months ago.

**Your AI tools forget everything. Every. Single. Session.**

---

## The Solution

```bash
npx context-keeper init
```

That's it. One command bootstraps a `.context/` folder in your project with:
- `decisions.md` — your architectural decisions  
- `conventions.md` — your team's coding patterns  
- `gotchas.md` — known pitfalls and workarounds  
- A SQLite database powering instant full-text search

From now on, your AI tools **remember**. Across sessions. Across team members.

---

## Quick Start

```bash
# Initialize in your project
npx context-keeper init

# Save a decision
ctx add --type decision --title "Use PostgreSQL" --content "Chose PostgreSQL over MySQL for better JSON support and full-text search capabilities"

# Or interactively
ctx add

# Search from terminal
ctx search "authentication pattern"

# List all memory
ctx list
```

---

## Works With Every AI Tool

### GitHub Copilot (VS Code Extension)
Install [Context Keeper](https://marketplace.visualstudio.com/items?itemName=context-keeper.context-keeper) from the VS Code Marketplace.

Then in Copilot Chat:
```
@ctx what's our authentication pattern?
@ctx show me all architectural decisions
@ctx conventions
```

### Claude Code (MCP)
Add to your `.claude/mcp.json`:
```json
{
  "mcpServers": {
    "context-keeper": {
      "command": "npx",
      "args": ["@context-keeper/mcp-server"]
    }
  }
}
```

Then Claude will automatically use tools like `search`, `remember`, `context`, `decisions`, and `conventions`.

### Cursor (MCP)
Add to your `cursor://settings/mcp`:
```json
{
  "context-keeper": {
    "command": "npx @context-keeper/mcp-server"
  }
}
```

### Continue.dev (MCP)
Add to `~/.continue/config.json`:
```json
{
  "experimental": {
    "modelContextProtocolServers": [{
      "transport": {
        "type": "stdio",
        "command": "npx",
        "args": ["@context-keeper/mcp-server"]
      }
    }]
  }
}
```

---

## Team Sharing via Git

```bash
# Commit your memory files (not the DB) to share with team
git add .context/decisions.md .context/conventions.md .context/gotchas.md
git commit -m "docs: add project memory"
```

Every team member's AI assistant now has the same context. The SQLite DB is gitignored (ephemeral), the markdown files are committed (canonical).

---

## MCP Tools

When used via MCP (Claude Code, Cursor, Continue), these tools are available:

| Tool | Description |
|------|-------------|
| `remember` | Save important project knowledge |
| `search` | Natural language search across all memory |
| `context` | Get full project context summary |
| `decisions` | List all architectural decisions |
| `conventions` | Get all coding conventions |

---

## CLI Reference

```
ctx init              Initialize in current project
ctx add               Add a memory entry (interactive)
ctx add --type <t>    Add non-interactively (decision|convention|gotcha|context|note)
ctx search <query>    Search memory
ctx list              List all entries
ctx list --type decision   Filter by type
ctx summary           Show memory summary
ctx delete <id>       Delete an entry
```

---

## Why Not [claude-mem](https://github.com/thedotmack/claude-mem)?

| Feature | claude-mem | context-keeper |
|---------|-----------|----------------|
| GitHub Copilot | ❌ | ✅ |
| Claude Code | ✅ | ✅ |
| Cursor | ❌ | ✅ |
| Continue.dev | ❌ | ✅ |
| Windsurf | ❌ | ✅ |
| License | AGPL-3.0 | **MIT** |
| Setup | Bun + Python + uv | **npx only** |
| Team sharing | ❌ | ✅ **Git-native** |
| Crypto token | $CMEM | ❌ None |

---

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md).

```bash
git clone https://github.com/yourusername/context-keeper
cd context-keeper
npm install
npm run build
```

---

## License

[MIT](LICENSE) — use it anywhere, even commercially.

---

<p align="center">
  Made for developers who are tired of their AI having amnesia.
  <br>
  <a href="https://github.com/yourusername/context-keeper/stargazers">⭐ Star if it helps you!</a>
</p>
