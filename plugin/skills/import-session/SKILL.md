---
name: import-session
description: Scan recent Claude Code and GitHub Copilot CLI session history for this project and import discovered decisions, patterns, gotchas, and file maps into context-keeper memory. Use when starting fresh on a project that has prior session history but an empty or sparse memory store.
---

# /import-session — Import Context From Past Sessions

Reads Claude Code and GitHub Copilot CLI session JSONL files for the current project, extracts reusable knowledge, and saves it into context-keeper memory so future sessions don't rediscover the same things.

## Usage
```
/import-session
/import-session 5
/import-session 10
/import-session all
/import-session all 10
```

| Form | Behavior |
|---|---|
| `/import-session` | Current project, last 5 sessions per source |
| `/import-session N` | Current project, last N sessions per source |
| `/import-session all` | Every project, last 5 sessions per source each |
| `/import-session all N` | Every project, last N sessions per source each |

## What it does
1. Finds recent sessions from **both** Claude Code and Copilot CLI — for the current project, or all projects if `all` is passed
2. Reads user prompts and assistant responses from each session
3. Identifies what is worth saving: decisions, patterns, gotchas, file maps, API endpoints, schema info
4. Deduplicates against existing memory with `search()`
5. Calls `remember()` for each new insight, tagged `"imported-session"`

---

## Instructions

When this skill is invoked, follow these steps exactly:

### Step 1 — Parse arguments and get project context

Parse the invocation arguments:
- If argument is `all` (or `all N`) → set `MODE=all`, `LIMIT=N` (default 5)
- Otherwise → set `MODE=project`, `LIMIT=N` (default 5)

Run:
```bash
pwd && git rev-parse --show-toplevel 2>/dev/null || pwd
```

You now have:
- `CWD` = current working directory
- `GIT_ROOT` = git repository root (fall back to CWD if not a git repo)

---

### Step 2 — Find Claude Code sessions

**If MODE=project** (default):

Claude Code stores project sessions at `~/.claude/projects/{slug}/*.jsonl` where slug = path with `/` → `-`:
```bash
SLUG=$(echo "$GIT_ROOT" | sed 's|/|-|g')
ls -t "$HOME/.claude/projects/$SLUG"/*.jsonl 2>/dev/null | head -N
```

**If MODE=all**:

Scan every project folder under `~/.claude/projects/`:
```bash
ls -td ~/.claude/projects/*/  # each is a project slug directory
```
For each project directory, take up to N most recent `.jsonl` files. Process all of them.

For each `.jsonl` file (either mode), extract:
- **User prompts**: `"type":"user"` where `isMeta` is not `true` and `message.content` is a plain string
- **Assistant text**: `"type":"assistant"` where content blocks have `"type":"text"`

---

### Step 3 — Find GitHub Copilot CLI sessions

**If MODE=project**:

Copilot stores all sessions in a flat directory — filter by project using `workspace.yaml`:
```bash
for dir in ~/.copilot/session-state/*/; do
  yaml="$dir/workspace.yaml"
  if [ -f "$yaml" ] && grep -q "git_root: $GIT_ROOT" "$yaml" 2>/dev/null; then
    echo "$dir"
  fi
done
```
Sort by `updated_at` in workspace.yaml and take the N most recent.

**If MODE=all**:

Take all session directories that have a `workspace.yaml` (skip those without — they have no project info):
```bash
ls -d ~/.copilot/session-state/*/  | while read dir; do
  [ -f "${dir}workspace.yaml" ] && echo "$dir"
done
```
For each, read the matching `{uuid}.jsonl` file — note the session's `git_root` from workspace.yaml so you can tag entries with the correct project path.

For each matching session (either mode), read `~/.copilot/session-state/{uuid}.jsonl`:
- **User prompts**: `"type":"user.message"` → `data.content` (plain string)
- **Assistant text**: `"type":"assistant.message"` → `data.content` (plain string)

Skip `tool.execution_start`, `tool.execution_complete`, `session.info`, `session.start` lines.

---

### Step 4 — Identify importable knowledge

For each session (from either source), synthesize what happened and find entries worth keeping. Ask yourself: **"Would knowing this save time in a future session?"**

| Pattern | Type | Example |
|---|---|---|
| Architectural choice made | `decision` | "Chose SQLite over Postgres because zero-install" |
| Bug found and fixed | `gotcha` | "HasLocation trait missing — run composer require" |
| Reusable solution | `pattern` | "All API routes follow REST + snake_case" |
| Abandoned approach | `rejected` | "Tried Redis cache — dropped, too complex for scale" |
| Project coding style | `convention` | "Always use `form.reset()` after submit" |
| Feature → file mapping | `file-map` | "Auth logic: auth_service.go + auth_handler.go" |
| API endpoint detail | `api-catalog` | "POST /teams/:id/members — requires admin role" |
| DB table or schema detail | `schema` | "members column is JSON string, not array" |

**Do NOT import:**
- Ephemeral task instructions ("implement X", "fix Y")
- File contents shown as context
- Tool call outputs or command results
- Status messages ("done", "working on it")
- Anything already in context-keeper memory

---

### Step 5 — Deduplicate

Before saving each candidate, call:
```
search(query: "{candidate title}")
```

Skip if a similar entry exists (score > 3). Don't flood the DB with duplicates.

---

### Step 6 — Save with remember()

For each new insight:
```
remember(
  type="...",
  title="...",       ← max 80 chars, self-descriptive
  content="...",     ← self-contained explanation, include context/reasoning
  tags=["imported-session", "{source}", "{project}"]
  ← source = "claude-code" or "copilot"
  ← project = basename of git_root (e.g. "my-app") — include when MODE=all so entries are traceable
)
```

Cap at **20 new entries per project** (or 20 total when MODE=project) — quality over quantity.

---

### Step 7 — Report results

Print a summary after finishing:

**MODE=project:**
```
/import-session complete  (project: {git_root})

Sources scanned:
  Claude Code: N sessions  (~/.claude/projects/{slug}/)
  Copilot CLI: M sessions  (~/.copilot/session-state/)

Results:
  Candidates identified: X
  Already in memory (skipped): Y
  New entries saved: Z

New entries:
  [decision]   Title...
  [gotcha]     Title...
  [file-map]   Title...

Tagged: imported-session, claude-code / copilot
```

**MODE=all:**
```
/import-session all complete

Projects found:
  Claude Code: P projects  (~/.claude/projects/)
  Copilot CLI: Q projects  (~/.copilot/session-state/)

Results per project:
  my-app          →  5 new entries  (3 claude-code, 2 copilot)
  be-weworship    →  8 new entries  (8 copilot)
  other-project   →  0 new entries  (all duplicates)

Total: Z new entries saved across all projects

Tagged: imported-session, {claude-code|copilot}, {project-name}
```

If no sessions are found for a source, say so explicitly so the user knows what was checked.
