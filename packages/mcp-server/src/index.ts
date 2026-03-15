#!/usr/bin/env node
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';
import { ContextKeeperStorage, resolveDbPath, resolveGlobalDbPath, resolveWorkspaceDbPath } from '@context-keeper/core';
import type { MemoryEntry } from '@context-keeper/core';
import express from 'express';
import path from 'path';
import fs from 'fs';

const PROJECT_ROOT = process.env.CONTEXT_KEEPER_ROOT || process.cwd();
const DB_PATH = resolveDbPath(PROJECT_ROOT);
const DASHBOARD_PORT = parseInt(process.env.CONTEXT_KEEPER_PORT || '7373', 10);
const WORKSPACE_DB_PATH = resolveWorkspaceDbPath(PROJECT_ROOT) ?? undefined;

// ── CLI hook modes ────────────────────────────────────────────────────────────

function readStdin(): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    process.stdin.on('data', (c: Buffer) => chunks.push(c));
    process.stdin.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
    process.stdin.on('error', reject);
  });
}

if (process.argv[2] === 'hook') {
  const hookName = process.argv[3];

  // ── auto-install: ensure better-sqlite3 is available ────────────────────────
  const pluginDir = path.dirname(path.dirname(process.argv[1])); // plugin/
  const pluginModules = path.join(pluginDir, 'node_modules');
  if (!fs.existsSync(pluginModules)) {
    try {
      const { execSync } = require('child_process');
      execSync('npm install --prefer-offline', { cwd: pluginDir, stdio: 'ignore' });
    } catch { /* non-fatal — will fail below if sqlite3 truly missing */ }
  }

  // ── session-start: inject compact memory index ──────────────────────────────
  if (hookName === 'session-start') {
    const s = new ContextKeeperStorage({ dbPath: DB_PATH, projectRoot: PROJECT_ROOT, globalDbPath: resolveGlobalDbPath(), workspaceDbPath: WORKSPACE_DB_PATH });
    const md = s.generateContextMd();
    const wsMd = s.generateWorkspaceContextMd();
    s.close();

    // Write session start timestamp for stop hook
    try {
      const ctxDir = path.join(PROJECT_ROOT, '.context');
      if (!fs.existsSync(ctxDir)) fs.mkdirSync(ctxDir, { recursive: true });
      fs.writeFileSync(path.join(ctxDir, 'session-start.tmp'), new Date().toISOString());
    } catch { /* non-fatal */ }

    let out = '# Project Memory (context-keeper)\n\n';

    if (md.startsWith('<!--')) {
      out +=
        'No memory saved yet.\n\n' +
        'Action → type guide (save proactively without being asked):\n' +
        '- bug / fix / error → type: "gotcha"\n' +
        '- implement / add / build → type: "decision" or "pattern" (if reusable)\n' +
        '- tried & abandoned approach → type: "rejected"\n' +
        '- knowledge shared across all projects → type: "workspace"\n';
    } else {
      out +=
        'Compact index — use `get([id])` for full details.\n\n' +
        md + '\n';
    }

    if (wsMd) {
      out += '\n## Workspace Memory (shared across projects)\n\n' + wsMd + '\n';
    }

    process.stdout.write(out);
    process.exit(0);
  }

  // ── stop: auto-save session summary ─────────────────────────────────────────
  if (hookName === 'stop') {
    const s = new ContextKeeperStorage({ dbPath: DB_PATH, projectRoot: PROJECT_ROOT, globalDbPath: resolveGlobalDbPath(), workspaceDbPath: WORKSPACE_DB_PATH });
    try {
      const tmpFile = path.join(PROJECT_ROOT, '.context', 'session-start.tmp');
      let since: string | null = null;
      try {
        since = fs.readFileSync(tmpFile, 'utf8').trim();
        fs.unlinkSync(tmpFile);
      } catch { /* no tmp file */ }

      if (since) {
        // Normalize JS ISO timestamp to SQLite datetime format for comparison
        const sinceForSqlite = since.replace('T', ' ').replace(/\.\d{3}Z$/, '');
        const sessionEntries = s.list(200).filter(e =>
          e.created_at >= sinceForSqlite && e.type !== 'session'
        );

        if (sessionEntries.length > 0) {
          const lines = sessionEntries.map(e =>
            `- [${e.type}] ${e.title}: ${e.content.split('\n')[0]}`
          ).join('\n');
          s.addSessionSummary(`${sessionEntries.length} entries saved this session:\n${lines}`);
        }
      }
    } catch { /* non-fatal */ }
    s.close();
    process.exit(0);
  }

  // ── async hooks (need stdin) ─────────────────────────────────────────────────
  (async () => {
    const raw = await readStdin().catch(() => '');
    let hookInput: Record<string, any> = {};
    try { hookInput = JSON.parse(raw); } catch { /* not JSON */ }

    // ── user-prompt: inject relevant memory + action hints (#5) ──────────────
    if (hookName === 'user-prompt') {
      const prompt: string = hookInput.prompt || hookInput.user_prompt || hookInput.message || raw.trim();
      if (!prompt || prompt.length < 15) process.exit(0);

      try {
        const s = new ContextKeeperStorage({ dbPath: DB_PATH, projectRoot: PROJECT_ROOT, workspaceDbPath: WORKSPACE_DB_PATH });
        const lc = prompt.toLowerCase();

        // #5 — detect action type and hint Claude on what type to save
        let typeHint = '';
        if (/\b(fix|bug|broken|error|crash|not working|failing|exception|undefined|null pointer|TypeError|ReferenceError)\b/.test(lc)) {
          typeHint = 'bug fix detected → save findings as type "gotcha" (what caused it + what fixed it)';
        } else if (/\b(again|same as|like before|repeated|every time|always when)\b/.test(lc)) {
          typeHint = 'repeated task — check patterns first: call patterns() or search(scope="all")';
        } else if (/\b(implement|add feature|create|build|integrate|set up|scaffold)\b/.test(lc)) {
          typeHint = 'implementation task → save reusable approach as type "pattern", key decision as type "decision"';
        } else if (/\b(refactor|restructure|reorganize|rename|move|migrate)\b/.test(lc)) {
          typeHint = 'refactor task → save reasoning as type "decision"';
        }

        // search local + workspace
        const localResults = s.search(prompt, 3);
        const wsResults = s.searchWorkspace(prompt, 2);
        s.close();

        let out = '';
        if (typeHint) out += `[context-keeper: ${typeHint}]\n`;
        const allResults = [...wsResults, ...localResults].slice(0, 4);
        if (allResults.length > 0) {
          const lines = allResults.map(r => {
            const project = wsResults.includes(r) && r.source ? ` [${path.basename(r.source)}]` : '';
            return `- [${r.type}] **${r.title}** [id:${r.id}]${project}: ${r.content.split('\n')[0]}`;
          }).join('\n');
          out += `[context-keeper: relevant memory]\n${lines}\n`;
        }
        if (out) process.stdout.write(out);
      } catch { /* non-fatal */ }
      process.exit(0);
    }

    // ── exit-plan-mode: remind Claude to save plan outcome ────────────────────
    if (hookName === 'exit-plan-mode') {
      process.stdout.write(
        '[context-keeper: Plan mode exited.\n' +
        '- If this plan was REJECTED → call remember(type="rejected", title="[Plan] ...", content="what was proposed + why you rejected it + what you chose instead")\n' +
        '- If this plan was ACCEPTED → it will be tracked via the implementation automatically]\n'
      );
      process.exit(0);
    }

    // ── post-tool-use: auto-capture Bash failures as gotchas ───────────────────
    if (hookName === 'post-tool-use') {
      const toolName: string = hookInput.tool_name || hookInput.toolName || '';
      if (toolName !== 'Bash') process.exit(0);

      const toolResponse = hookInput.tool_response || {};
      const exitCode: number | undefined = toolResponse.exit_code ?? hookInput.exit_code;
      const stderr: string = (toolResponse.stderr || hookInput.stderr || '').trim();
      const stdout: string = (toolResponse.stdout || hookInput.stdout || '').trim();
      const command: string = (hookInput.tool_input?.command || hookInput.command || '').slice(0, 120);

      const isFailed = exitCode !== undefined ? exitCode !== 0 : stderr.length > 20;
      const errorText = stderr || stdout;
      if (!isFailed || errorText.length < 20) process.exit(0);

      // Skip noisy/transient errors not worth saving
      const skipPatterns = ['interrupt', 'killed', 'SIGTERM', 'SIGINT'];
      if (skipPatterns.some(p => errorText.toLowerCase().includes(p.toLowerCase()))) process.exit(0);

      try {
        const s = new ContextKeeperStorage({ dbPath: DB_PATH, projectRoot: PROJECT_ROOT });
        const firstLine = errorText.split('\n')[0].slice(0, 60);
        const titleText = `[Auto] ${firstLine}`;

        // Deduplicate — skip if very similar entry already exists
        const existing = s.search(firstLine, 1);
        if (existing.length > 0 && existing[0].score > 3) { s.close(); process.exit(0); }

        s.add({
          type: 'gotcha',
          title: titleText.slice(0, 80),
          content: `Command: ${command}\n\nError:\n${errorText.slice(0, 500)}`,
          tags: ['auto-captured', 'tool-failure'],
          source: 'post-tool-use-hook',
        });
        s.close();
      } catch { /* non-fatal */ }
      process.exit(0);
    }
  })().catch(() => process.exit(0));
}

// SSE clients for live dashboard
const sseClients: express.Response[] = [];

function broadcast(event: string, data: unknown) {
  const payload = `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
  sseClients.forEach(res => res.write(payload));
}

const storage = new ContextKeeperStorage({ dbPath: DB_PATH, projectRoot: PROJECT_ROOT, globalDbPath: resolveGlobalDbPath(), workspaceDbPath: WORKSPACE_DB_PATH });

// ── Dashboard HTTP server ───────────────────────────────────────────────────

function startDashboard() {
  const app = express();
  app.use(express.json());

  app.get('/', (_req, res) => {
    res.send(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>context-keeper</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0f0f0f; color: #e0e0e0; height: 100vh; display: flex; flex-direction: column; }
    header { background: #1a1a1a; border-bottom: 1px solid #2a2a2a; padding: 12px 20px; display: flex; align-items: center; gap: 12px; }
    header h1 { font-size: 16px; font-weight: 600; color: #fff; }
    .status { display: flex; align-items: center; gap: 6px; font-size: 12px; color: #888; margin-left: auto; }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: #4caf50; animation: pulse 2s infinite; }
    @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
    .tabs { background: #1a1a1a; border-bottom: 1px solid #2a2a2a; display: flex; gap: 0; padding: 0 20px; }
    .tab { padding: 10px 16px; font-size: 13px; cursor: pointer; border-bottom: 2px solid transparent; color: #888; transition: all 0.15s; }
    .tab.active { color: #fff; border-bottom-color: #4caf50; }
    .tab:hover { color: #ccc; }
    main { flex: 1; overflow-y: auto; padding: 20px; }
    .empty { color: #555; font-size: 13px; text-align: center; padding: 40px; }
    .entry { background: #1a1a1a; border: 1px solid #2a2a2a; border-radius: 8px; padding: 14px 16px; margin-bottom: 10px; }
    .entry-header { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
    .badge { font-size: 10px; font-weight: 600; padding: 2px 7px; border-radius: 20px; text-transform: uppercase; letter-spacing: 0.05em; }
    .badge-decision { background: #1e3a5f; color: #64b5f6; }
    .badge-convention { background: #1a3a1a; color: #81c784; }
    .badge-gotcha { background: #3a2a0a; color: #ffb74d; }
    .badge-context { background: #2a1a3a; color: #ce93d8; }
    .badge-note { background: #2a2a2a; color: #9e9e9e; }
    .badge-rejected { background: #3a1a1a; color: #ef5350; }
    .badge-session { background: #1a2a3a; color: #4dd0e1; }
    .badge-pattern { background: #1a3a2a; color: #69f0ae; }
    .badge-workspace { background: #2a2a0a; color: #fff176; }
    .entry-title { font-size: 14px; font-weight: 500; color: #fff; }
    .entry-content { font-size: 13px; color: #aaa; line-height: 1.5; white-space: pre-wrap; }
    .entry-meta { font-size: 11px; color: #555; margin-top: 6px; }
    .entry-tags { display: flex; gap: 4px; margin-top: 6px; flex-wrap: wrap; }
    .tag { font-size: 11px; background: #222; color: #777; padding: 2px 6px; border-radius: 4px; }
    .live-new { animation: fadeIn 0.4s ease; }
    @keyframes fadeIn { from { opacity: 0; transform: translateY(-6px); } to { opacity: 1; transform: translateY(0); } }
    #tab-live .entry:first-child { border-color: #2d4a2d; }
  </style>
</head>
<body>
  <header>
    <h1>🧠 context-keeper</h1>
    <div class="status"><div class="dot"></div><span id="status-text">Connected</span></div>
  </header>
  <div class="tabs">
    <div class="tab active" onclick="showTab('live')">Live Feed</div>
    <div class="tab" onclick="showTab('decisions')">Decisions</div>
    <div class="tab" onclick="showTab('conventions')">Conventions</div>
    <div class="tab" onclick="showTab('gotchas')">Gotchas</div>
    <div class="tab" onclick="showTab('patterns')">Patterns</div>
    <div class="tab" onclick="showTab('rejected')">Rejected</div>
    <div class="tab" onclick="showTab('workspace')">Workspace</div>
    <div class="tab" onclick="showTab('sessions')">Sessions</div>
    <div class="tab" onclick="showTab('all')">All Memory</div>
  </div>
  <main>
    <div id="tab-live"><div class="empty">Waiting for Claude to save memory…</div></div>
    <div id="tab-decisions" style="display:none"></div>
    <div id="tab-conventions" style="display:none"></div>
    <div id="tab-gotchas" style="display:none"></div>
    <div id="tab-patterns" style="display:none"></div>
    <div id="tab-rejected" style="display:none"></div>
    <div id="tab-workspace" style="display:none"></div>
    <div id="tab-sessions" style="display:none"></div>
    <div id="tab-all" style="display:none"></div>
  </main>
  <script>
    let currentTab = 'live';

    function badge(type) {
      return '<span class="badge badge-' + type + '">' + type + '</span>';
    }

    function renderEntry(e, isNew) {
      const date = new Date(e.updated_at).toLocaleString();
      const tags = e.tags && e.tags.length ? '<div class="entry-tags">' + e.tags.map(t => '<span class="tag">' + t + '</span>').join('') + '</div>' : '';
      const supersedes = e.supersedes_id ? '<span style="color:#f0a050;font-size:11px">↻ Updated (was #' + e.supersedes_id + ')</span>' : '';
      const reason = e.change_reason ? '<div style="font-size:12px;color:#f0a050;margin-top:4px">Why changed: ' + e.change_reason + '</div>' : '';
      return '<div class="entry' + (isNew ? ' live-new' : '') + '">' +
        '<div class="entry-header">' + badge(e.type) + '<span class="entry-title">' + e.title + '</span>' + (supersedes ? supersedes : '') + '</div>' +
        '<div class="entry-content">' + e.content + '</div>' +
        reason + tags +
        '<div class="entry-meta">#' + e.id + ' · ' + date + '</div>' +
        '</div>';
    }

    function showTab(tab) {
      document.querySelectorAll('.tab').forEach((t, i) => t.classList.toggle('active', ['live','decisions','conventions','gotchas','patterns','rejected','workspace','sessions','all'][i] === tab));
      document.querySelectorAll('main > div').forEach(d => d.style.display = 'none');
      document.getElementById('tab-' + tab).style.display = 'block';
      currentTab = tab;
      if (tab !== 'live') loadTab(tab);
    }

    async function loadTab(tab) {
      const typeMap = { decisions: 'decision', conventions: 'convention', gotchas: 'gotcha', patterns: 'pattern', rejected: 'rejected', workspace: 'workspace', sessions: 'session', all: '' };
      const type = typeMap[tab] || '';
      const url = '/api/memory' + (type ? '?type=' + type : '');
      const res = await fetch(url);
      const entries = await res.json();
      const el = document.getElementById('tab-' + tab);
      if (!entries.length) { el.innerHTML = '<div class="empty">Nothing saved yet.</div>'; return; }
      el.innerHTML = entries.map(e => renderEntry(e, false)).join('');
    }

    // SSE live feed
    const evtSource = new EventSource('/events');
    evtSource.addEventListener('memory', e => {
      const entry = JSON.parse(e.data);
      const liveTab = document.getElementById('tab-live');
      const empty = liveTab.querySelector('.empty');
      if (empty) empty.remove();
      liveTab.insertAdjacentHTML('afterbegin', renderEntry(entry, true));
    });
    evtSource.onerror = () => {
      document.getElementById('status-text').textContent = 'Disconnected';
      document.querySelector('.dot').style.background = '#f44336';
    };
  </script>
</body>
</html>`);
  });

  // SSE endpoint
  app.get('/events', (req, res) => {
    res.setHeader('Content-Type', 'text/event-stream');
    res.setHeader('Cache-Control', 'no-cache');
    res.setHeader('Connection', 'keep-alive');
    res.setHeader('Access-Control-Allow-Origin', '*');
    res.write('retry: 3000\n\n');
    sseClients.push(res);
    req.on('close', () => {
      const idx = sseClients.indexOf(res);
      if (idx > -1) sseClients.splice(idx, 1);
    });
  });

  // API endpoints
  app.get('/api/memory', (req, res) => {
    const type = req.query.type as string | undefined;
    const entries = type
      ? storage.list(200, type as MemoryEntry['type'])
      : storage.list(200);
    res.json(entries);
  });

  app.get('/api/memory/:id/history', (req, res) => {
    const history = storage.history(parseInt(req.params.id));
    if (!history) { res.status(404).json({ error: 'Not found' }); return; }
    res.json(history);
  });

  app.listen(DASHBOARD_PORT, () => {
    process.stderr.write(`context-keeper dashboard: http://localhost:${DASHBOARD_PORT}\n`);
  });
}

// ── MCP Server ──────────────────────────────────────────────────────────────

const server = new Server(
  { name: 'context-keeper', version: '0.2.0' },
  { capabilities: { tools: {} } },
);

server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [
    {
      name: 'remember',
      description: `ALWAYS call this tool proactively — without being asked.

Action → type guide:
- fix / bug / error / crash → "gotcha" (save: what caused it + what fixed it)
- implement / add / build / integrate → "pattern" if reusable step-by-step, "decision" if architectural
- refactor / restructure / migrate → "decision"
- tried & abandoned approach → "rejected" (save: what was tried + why rejected + what was chosen)
- knowledge shared across all projects in workspace (API shape, auth flow, env vars) → "workspace"
- coding convention specific to this project → "convention"

Save first, continue working. If in doubt, save it.`,
      inputSchema: {
        type: 'object',
        properties: {
          type: {
            type: 'string',
            enum: ['decision', 'convention', 'gotcha', 'context', 'note', 'rejected', 'pattern', 'workspace', 'session'],
            description: 'See action guide above',
          },
          title: { type: 'string', description: 'Short descriptive title (max 80 chars)' },
          content: { type: 'string', description: 'Full explanation including reasoning' },
          tags: { type: 'array', items: { type: 'string' }, description: 'Optional tags for categorization' },
        },
        required: ['type', 'title', 'content'],
      },
    },
    {
      name: 'update_decision',
      description: `Update an existing decision when it changes. Use this INSTEAD of remember when you are changing a previous decision.
IMPORTANT: Always include why_changed — this is the most valuable field for future context.
The old decision is preserved as history (not deleted).`,
      inputSchema: {
        type: 'object',
        properties: {
          id: { type: 'number', description: 'ID of the existing decision to update (from [id:N] in remember response)' },
          title: { type: 'string', description: 'New title' },
          content: { type: 'string', description: 'New content explaining the updated decision' },
          why_changed: { type: 'string', description: 'Why the decision changed (REQUIRED — key context for future sessions)' },
          tags: { type: 'array', items: { type: 'string' } },
        },
        required: ['id', 'title', 'content', 'why_changed'],
      },
    },
    {
      name: 'search',
      description: 'Search project memory using natural language. Call this before starting a new task to check for relevant prior decisions or context. Use scope="all" to search across all your projects.',
      inputSchema: {
        type: 'object',
        properties: {
          query: { type: 'string', description: 'Natural language search query' },
          limit: { type: 'number', description: 'Max results (default: 5)', default: 5 },
          scope: {
            type: 'string',
            enum: ['local', 'workspace', 'global', 'all'],
            description: 'local=this project only (default), workspace=monorepo shared memory, global=all projects, all=everywhere',
            default: 'local',
          },
        },
        required: ['query'],
      },
    },
    {
      name: 'context',
      description: `Load project memory. Returns a compact index by default (title + first line per entry) to minimize token usage.
Use verbose=true only if you need full content for all entries at once.
Prefer: call context() to get the index, then call get() with specific IDs for details.`,
      inputSchema: {
        type: 'object',
        properties: {
          type: {
            type: 'string',
            enum: ['decision', 'convention', 'gotcha', 'context', 'note', 'rejected', 'pattern', 'workspace', 'session', 'all'],
            description: 'Filter by type (default: all)',
            default: 'all',
          },
          verbose: {
            type: 'boolean',
            description: 'If true, return full content for all entries. Default false (compact index only).',
            default: false,
          },
        },
      },
    },
    {
      name: 'get',
      description: 'Fetch full content for one or more memory entries by ID. Use after calling context() to get details on specific entries.',
      inputSchema: {
        type: 'object',
        properties: {
          ids: {
            type: 'array',
            items: { type: 'number' },
            description: 'Array of entry IDs to fetch full content for',
          },
        },
        required: ['ids'],
      },
    },
    {
      name: 'history',
      description: 'Get the full change history of a decision. Use when the user asks "why did we change X?" or "what was the old approach?".',
      inputSchema: {
        type: 'object',
        properties: {
          id: { type: 'number', description: 'ID of the decision to get history for' },
        },
        required: ['id'],
      },
    },
    {
      name: 'decisions',
      description: 'List all architectural decisions. Use when the user asks about architecture, technology choices, or design decisions.',
      inputSchema: { type: 'object', properties: {} },
    },
    {
      name: 'conventions',
      description: 'Get all coding conventions. Call this before writing code to ensure you follow established patterns.',
      inputSchema: { type: 'object', properties: {} },
    },
    {
      name: 'patterns',
      description: 'List all saved solution patterns — reusable step-by-step approaches for repeated tasks. Call this FIRST when asked to do something you may have done before.',
      inputSchema: {
        type: 'object',
        properties: {
          search: { type: 'string', description: 'Optional keyword to filter patterns' },
        },
      },
    },
  ],
}));

server.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args } = request.params;

  try {
    switch (name) {
      case 'remember': {
        const entry = storage.add({
          type: (args?.type as MemoryEntry['type']) || 'note',
          title: String(args?.title || ''),
          content: String(args?.content || ''),
          tags: (args?.tags as string[]) || [],
        });
        broadcast('memory', entry);
        return {
          content: [{
            type: 'text',
            text: `✅ Saved [${entry.type}] ${entry.title} [id:${entry.id}]`,
          }],
        };
      }

      case 'update_decision': {
        const updated = storage.updateDecision(
          Number(args?.id),
          {
            title: String(args?.title || ''),
            content: String(args?.content || ''),
            tags: (args?.tags as string[]) || [],
          },
          String(args?.why_changed || ''),
        );
        if (!updated) {
          return { content: [{ type: 'text', text: `❌ Decision #${args?.id} not found` }], isError: true };
        }
        broadcast('memory', updated);
        return {
          content: [{
            type: 'text',
            text: `✅ Updated decision [id:${updated.id}] — previous version preserved as history [id:${updated.supersedes_id}]`,
          }],
        };
      }

      case 'search': {
        const query = String(args?.query || '');
        const limit = Number(args?.limit) || 5;
        const scope = String(args?.scope || 'local');

        let results;
        if (scope === 'all') results = storage.searchAll(query, limit);
        else if (scope === 'global') results = storage.searchGlobal(query, limit);
        else if (scope === 'workspace') results = storage.searchWorkspace(query, limit);
        else results = storage.search(query, limit);

        if (results.length === 0) {
          return { content: [{ type: 'text', text: 'No matching memory found.' }] };
        }
        const text = results.map(r => {
          const origin = scope !== 'local' && r.source && r.source !== 'post-tool-use-hook' && r.source !== 'stop-hook'
            ? ` [from: ${r.source}]` : '';
          return `[${r.type.toUpperCase()}] ${r.title} [id:${r.id}]${origin}\n${r.content}${r.tags.length ? `\nTags: ${r.tags.join(', ')}` : ''}`;
        }).join('\n\n---\n\n');
        return { content: [{ type: 'text', text }] };
      }

      case 'context': {
        const type = args?.type as string;
        const verbose = args?.verbose === true;
        const entries = type && type !== 'all'
          ? storage.list(100, type as MemoryEntry['type'])
          : storage.list(100);

        if (entries.length === 0) {
          return {
            content: [{ type: 'text', text: 'No project memory yet. Call `remember` to start saving decisions, conventions, and gotchas.' }],
          };
        }

        if (verbose) {
          const grouped = entries.reduce((acc, e) => {
            if (!acc[e.type]) acc[e.type] = [];
            acc[e.type].push(e);
            return acc;
          }, {} as Record<string, typeof entries>);
          const sections = Object.entries(grouped).map(([t, items]) =>
            `## ${t.charAt(0).toUpperCase() + t.slice(1)}s\n\n` +
            items.map(e => `### ${e.title} [id:${e.id}]\n${e.content}`).join('\n\n')
          ).join('\n\n');
          return { content: [{ type: 'text', text: `# Project Memory (full)\n\n${sections}` }] };
        }

        // Compact index: title + first line + id only
        const md = storage.generateContextMd();
        return {
          content: [{
            type: 'text',
            text: `# Project Memory Index (${entries.length} entries)\n\nUse \`get\` with IDs for full details.\n\n${md}`,
          }],
        };
      }

      case 'get': {
        const ids = (args?.ids as number[]) || [];
        if (!ids.length) {
          return { content: [{ type: 'text', text: 'Provide at least one ID.' }], isError: true };
        }
        const results = ids.map(id => storage.getById(id)).filter(Boolean) as MemoryEntry[];
        if (!results.length) {
          return { content: [{ type: 'text', text: `No entries found for ids: ${ids.join(', ')}` }] };
        }
        const text = results.map(e =>
          `### ${e.title} [${e.type}] [id:${e.id}]\n${e.content}${e.tags.length ? `\n*Tags: ${e.tags.join(', ')}*` : ''}`
        ).join('\n\n---\n\n');
        return { content: [{ type: 'text', text }] };
      }

      case 'history': {
        const h = storage.history(Number(args?.id));
        if (!h) {
          return { content: [{ type: 'text', text: `❌ Decision #${args?.id} not found` }], isError: true };
        }
        const lines = [
          `# Decision History: ${h.current.title}`,
          '',
          `**Current** [id:${h.current.id}]`,
          h.current.content,
          '',
          ...h.history.map((old, i) => [
            `**v${h.history.length - i}** [id:${old.id}] — superseded on ${old.updated_at}`,
            old.change_reason ? `*Reason changed: ${old.change_reason}*` : '',
            old.content,
          ].filter(Boolean).join('\n')),
        ];
        return { content: [{ type: 'text', text: lines.join('\n') }] };
      }

      case 'decisions': {
        const entries = storage.list(50, 'decision');
        if (entries.length === 0) {
          return { content: [{ type: 'text', text: 'No architectural decisions recorded yet.' }] };
        }
        const text = entries.map(e =>
          `### ${e.title} [id:${e.id}]\n${e.content}${e.tags.length ? `\n*Tags: ${e.tags.join(', ')}*` : ''}`
        ).join('\n\n---\n\n');
        return { content: [{ type: 'text', text: `# Architectural Decisions\n\n${text}` }] };
      }

      case 'conventions': {
        const entries = storage.list(50, 'convention');
        if (entries.length === 0) {
          return { content: [{ type: 'text', text: 'No conventions recorded yet.' }] };
        }
        const text = entries.map(e => `### ${e.title} [id:${e.id}]\n${e.content}`).join('\n\n---\n\n');
        return { content: [{ type: 'text', text: `# Coding Conventions\n\n${text}` }] };
      }

      case 'patterns': {
        const q = args?.search as string | undefined;
        const entries = q
          ? storage.search(q, 20).filter(r => r.type === 'pattern')
          : storage.list(50, 'pattern');
        if (entries.length === 0) {
          return { content: [{ type: 'text', text: 'No patterns saved yet. Save reusable step-by-step solutions with remember(type="pattern", ...).' }] };
        }
        const text = entries.map(e =>
          `### ${e.title} [id:${e.id}]\n${e.content}${e.tags.length ? `\n*Tags: ${e.tags.join(', ')}*` : ''}`
        ).join('\n\n---\n\n');
        return { content: [{ type: 'text', text: `# Solution Patterns\n\n${text}` }] };
      }

      default:
        return { content: [{ type: 'text', text: `Unknown tool: ${name}` }], isError: true };
    }
  } catch (error) {
    return {
      content: [{ type: 'text', text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
      isError: true,
    };
  }
});

async function main() {
  startDashboard();
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch(err => {
  process.stderr.write(`Fatal: ${err}\n`);
  process.exit(1);
});
