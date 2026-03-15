import Database from 'better-sqlite3';
import path from 'path';
import fs from 'fs';
import os from 'os';
import type { MemoryEntry, SearchResult, ContextKeeperConfig, DecisionHistory } from './types';

const SCHEMA = `
  CREATE TABLE IF NOT EXISTS memory (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    type          TEXT NOT NULL DEFAULT 'note',
    title         TEXT NOT NULL,
    content       TEXT NOT NULL,
    tags          TEXT NOT NULL DEFAULT '[]',
    source        TEXT,
    status        TEXT NOT NULL DEFAULT 'active',
    supersedes_id INTEGER REFERENCES memory(id),
    change_reason TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
  );

  CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    title, content, tags,
    content='memory',
    content_rowid='id'
  );

  CREATE TRIGGER IF NOT EXISTS memory_ai AFTER INSERT ON memory BEGIN
    INSERT INTO memory_fts(rowid, title, content, tags)
    VALUES (new.id, new.title, new.content, new.tags);
  END;

  CREATE TRIGGER IF NOT EXISTS memory_au AFTER UPDATE ON memory BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, title, content, tags)
    VALUES ('delete', old.id, old.title, old.content, old.tags);
    INSERT INTO memory_fts(rowid, title, content, tags)
    VALUES (new.id, new.title, new.content, new.tags);
  END;

  CREATE TRIGGER IF NOT EXISTS memory_ad AFTER DELETE ON memory BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, title, content, tags)
    VALUES ('delete', old.id, old.title, old.content, old.tags);
  END;
`;

/** Types mirrored to workspace DB — cross-project architectural knowledge */
const WORKSPACE_MIRROR_TYPES: MemoryEntry['type'][] = ['decision', 'pattern', 'workspace', 'rejected'];

export class ContextKeeperStorage {
  private db: Database.Database;
  private globalDb?: Database.Database;
  private workspaceDb?: Database.Database;
  private config: ContextKeeperConfig;

  constructor(config: ContextKeeperConfig) {
    this.config = config;
    const dir = path.dirname(config.dbPath);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    this.db = new Database(config.dbPath);
    this.db.pragma('journal_mode = WAL');
    this.db.exec(SCHEMA);
    this.migrate();

    if (config.globalDbPath) {
      const globalDir = path.dirname(config.globalDbPath);
      if (!fs.existsSync(globalDir)) fs.mkdirSync(globalDir, { recursive: true });
      this.globalDb = new Database(config.globalDbPath);
      this.globalDb.pragma('journal_mode = WAL');
      this.globalDb.exec(SCHEMA);
    }

    if (config.workspaceDbPath) {
      const wsDir = path.dirname(config.workspaceDbPath);
      if (!fs.existsSync(wsDir)) fs.mkdirSync(wsDir, { recursive: true });
      this.workspaceDb = new Database(config.workspaceDbPath);
      this.workspaceDb.pragma('journal_mode = WAL');
      this.workspaceDb.exec(SCHEMA);
    }
  }

  /** Add new columns if upgrading from older DB schema */
  private migrate(): void {
    const cols = (this.db.prepare(`PRAGMA table_info(memory)`).all() as any[]).map(r => r.name);
    if (!cols.includes('status')) {
      this.db.exec(`ALTER TABLE memory ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`);
    }
    if (!cols.includes('supersedes_id')) {
      this.db.exec(`ALTER TABLE memory ADD COLUMN supersedes_id INTEGER`);
    }
    if (!cols.includes('change_reason')) {
      this.db.exec(`ALTER TABLE memory ADD COLUMN change_reason TEXT`);
    }
  }

  add(entry: Omit<MemoryEntry, 'id' | 'created_at' | 'updated_at' | 'status'>): MemoryEntry {
    const stmt = this.db.prepare(`
      INSERT INTO memory (type, title, content, tags, source, supersedes_id, change_reason)
      VALUES (@type, @title, @content, @tags, @source, @supersedes_id, @change_reason)
    `);
    const params = {
      type: entry.type,
      title: entry.title,
      content: entry.content,
      tags: JSON.stringify(entry.tags),
      source: entry.source ?? null,
      supersedes_id: entry.supersedes_id ?? null,
      change_reason: entry.change_reason ?? null,
    };
    const result = stmt.run(params);

    // Mirror to global DB — tag with project root so cross-project search knows the origin
    if (this.globalDb) {
      try {
        this.globalDb.prepare(`
          INSERT INTO memory (type, title, content, tags, source, supersedes_id, change_reason)
          VALUES (@type, @title, @content, @tags, @source, @supersedes_id, @change_reason)
        `).run({ ...params, source: this.config.projectRoot });
      } catch { /* global DB write failure is non-fatal */ }
    }

    // Mirror workspace-relevant types to workspace DB
    if (this.workspaceDb && WORKSPACE_MIRROR_TYPES.includes(entry.type as MemoryEntry['type'])) {
      try {
        this.workspaceDb.prepare(`
          INSERT INTO memory (type, title, content, tags, source, supersedes_id, change_reason)
          VALUES (@type, @title, @content, @tags, @source, @supersedes_id, @change_reason)
        `).run({ ...params, source: this.config.projectRoot });
      } catch { /* workspace DB write failure is non-fatal */ }
    }

    return this.getById(result.lastInsertRowid as number)!;
  }

  /**
   * Update a decision: marks the old entry as 'superseded' and creates a new active entry.
   * Returns the new entry. The old entry is preserved in history.
   */
  updateDecision(
    supersedes_id: number,
    newEntry: Pick<MemoryEntry, 'title' | 'content' | 'tags'>,
    change_reason: string,
  ): MemoryEntry {
    const old = this.getById(supersedes_id);
    if (!old) throw new Error(`Entry #${supersedes_id} not found`);

    return this.db.transaction(() => {
      // Mark old entry as superseded
      this.db.prepare(`UPDATE memory SET status = 'superseded', updated_at = datetime('now') WHERE id = ?`).run(supersedes_id);
      // Create new entry that supersedes the old one
      return this.add({
        type: old.type,
        title: newEntry.title,
        content: newEntry.content,
        tags: newEntry.tags,
        supersedes_id,
        change_reason,
      });
    })();
  }

  getById(id: number): MemoryEntry | undefined {
    const row = this.db.prepare('SELECT * FROM memory WHERE id = ?').get(id) as any;
    return row ? this.deserialize(row) : undefined;
  }

  /**
   * Search only the global DB — returns entries from OTHER projects (excludes current project).
   */
  searchGlobal(query: string, limit = 10): SearchResult[] {
    if (!this.globalDb) return [];
    const sanitized = query.replace(/['"*]/g, ' ').trim();
    if (!sanitized) return [];

    const rows = this.globalDb.prepare(`
      SELECT m.*, memory_fts.rank AS score
      FROM memory_fts
      JOIN memory m ON memory_fts.rowid = m.id
      WHERE memory_fts MATCH ?
      AND m.status = 'active'
      AND (m.source IS NULL OR m.source != ?)
      ORDER BY rank
      LIMIT ?
    `).all(`${sanitized}*`, this.config.projectRoot, limit) as any[];

    return rows.map(row => ({ ...this.deserialize(row), score: Math.abs(row.score) }));
  }

  /**
   * Search the workspace DB — cross-project architectural knowledge.
   */
  searchWorkspace(query: string, limit = 10): SearchResult[] {
    if (!this.workspaceDb) return [];
    const sanitized = query.replace(/['"*]/g, ' ').trim();
    if (!sanitized) return [];

    const rows = this.workspaceDb.prepare(`
      SELECT m.*, memory_fts.rank AS score
      FROM memory_fts
      JOIN memory m ON memory_fts.rowid = m.id
      WHERE memory_fts MATCH ?
      AND m.status = 'active'
      ORDER BY rank
      LIMIT ?
    `).all(`${sanitized}*`, limit) as any[];

    return rows.map(row => ({ ...this.deserialize(row), score: Math.abs(row.score) }));
  }

  /**
   * Generate a compact markdown snapshot of workspace memory (cross-project entries).
   * Shows which project each entry came from.
   */
  generateWorkspaceContextMd(): string | null {
    if (!this.workspaceDb) return null;
    const rows = this.workspaceDb.prepare(
      `SELECT * FROM memory WHERE status = 'active' ORDER BY type, updated_at DESC LIMIT 200`
    ).all() as any[];
    if (rows.length === 0) return null;

    const entries = rows.map(r => this.deserialize(r));
    const grouped = entries.reduce((acc, e) => {
      if (!acc[e.type]) acc[e.type] = [];
      acc[e.type].push(e);
      return acc;
    }, {} as Record<string, MemoryEntry[]>);

    const order: MemoryEntry['type'][] = ['workspace', 'decision', 'pattern', 'rejected'];
    const sections = order
      .filter(t => grouped[t]?.length)
      .map(t => {
        const header = `### Workspace ${t.charAt(0).toUpperCase() + t.slice(1)}s`;
        const items = grouped[t].map(e => {
          const project = e.source ? path.basename(e.source) : 'shared';
          return `- **${e.title}** [id:${e.id}] [${project}]: ${e.content.split('\n')[0]}`;
        }).join('\n');
        return `${header}\n${items}`;
      });

    return sections.length ? sections.join('\n\n') : null;
  }

  /**
   * Search local, workspace, and global DBs, merging results.
   * Workspace first (most relevant cross-project), then local, then other projects.
   */
  searchAll(query: string, limit = 10): SearchResult[] {
    const local = this.search(query, limit);
    const workspace = this.searchWorkspace(query, Math.ceil(limit / 2));
    const global = this.searchGlobal(query, Math.ceil(limit / 2));
    return [...workspace, ...local, ...global]
      .sort((a, b) => b.score - a.score)
      .slice(0, limit);
  }

  /**
   * Search active memory entries using full-text search.
   */
  search(query: string, limit = 10, includeSuperseded = false): SearchResult[] {
    const sanitized = query.replace(/['"*]/g, ' ').trim();
    const statusFilter = includeSuperseded ? '' : `AND m.status = 'active'`;

    if (!sanitized) {
      return this.list(limit).map(e => ({ ...e, score: 1 }));
    }

    const rows = this.db.prepare(`
      SELECT m.*, memory_fts.rank AS score
      FROM memory_fts
      JOIN memory m ON memory_fts.rowid = m.id
      WHERE memory_fts MATCH ?
      ${statusFilter}
      ORDER BY rank
      LIMIT ?
    `).all(`${sanitized}*`, limit) as any[];

    return rows.map(row => ({
      ...this.deserialize(row),
      score: Math.abs(row.score),
    }));
  }

  /**
   * Get the full decision history for an entry (current + all superseded ancestors).
   */
  history(id: number): DecisionHistory | undefined {
    const current = this.getById(id);
    if (!current) return undefined;

    const historyEntries: MemoryEntry[] = [];
    let cursor: MemoryEntry | undefined = current;

    // Walk the supersedes chain backward
    while (cursor?.supersedes_id) {
      const prev = this.getById(cursor.supersedes_id);
      if (!prev) break;
      historyEntries.push(prev);
      cursor = prev;
    }

    return { current, history: historyEntries };
  }

  /**
   * List active entries, optionally filtered by type.
   */
  list(limit = 50, type?: MemoryEntry['type'], includeSuperseded = false): MemoryEntry[] {
    const statusFilter = includeSuperseded ? '' : `AND status = 'active'`;
    let rows;
    if (type) {
      rows = this.db.prepare(`SELECT * FROM memory WHERE type = ? ${statusFilter} ORDER BY updated_at DESC LIMIT ?`).all(type, limit);
    } else {
      rows = this.db.prepare(`SELECT * FROM memory WHERE 1=1 ${statusFilter} ORDER BY updated_at DESC LIMIT ?`).all(limit);
    }
    return (rows as any[]).map(row => this.deserialize(row));
  }

  delete(id: number): boolean {
    const result = this.db.prepare('DELETE FROM memory WHERE id = ?').run(id);
    return result.changes > 0;
  }

  /**
   * Save an auto-generated session summary (called by Stop hook).
   */
  addSessionSummary(content: string, tags: string[] = []): MemoryEntry {
    const date = new Date().toISOString().split('T')[0];
    const time = new Date().toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
    return this.add({
      type: 'session',
      title: `Session ${date} ${time}`,
      content,
      tags: ['auto-saved', ...tags],
      source: 'stop-hook',
    });
  }

  summary(): string {
    const counts = this.db.prepare(`
      SELECT type, COUNT(*) as count FROM memory WHERE status = 'active' GROUP BY type
    `).all() as { type: string; count: number }[];

    const superseded = (this.db.prepare(`SELECT COUNT(*) as n FROM memory WHERE status = 'superseded'`).get() as any).n;

    if (counts.length === 0) return 'No project memory yet.';

    const lines = counts.map(r => `  ${r.type}: ${r.count} entries`);
    const total = counts.reduce((s, r) => s + r.count, 0);
    const historyNote = superseded > 0 ? `\n  (+ ${superseded} superseded entries in history)` : '';
    return `Project Memory Summary (${total} active):\n${lines.join('\n')}${historyNote}`;
  }

  /**
   * Generate a compact markdown snapshot of all active memory.
   * Used for injecting into CLAUDE.md / system prompts.
   */
  generateContextMd(): string {
    const entries = this.list(200);
    if (entries.length === 0) {
      return '<!-- No project memory yet. Use context-keeper MCP tools to start building memory. -->';
    }

    const grouped = entries.reduce((acc, e) => {
      if (!acc[e.type]) acc[e.type] = [];
      acc[e.type].push(e);
      return acc;
    }, {} as Record<string, MemoryEntry[]>);

    const order: MemoryEntry['type'][] = ['workspace', 'decision', 'pattern', 'convention', 'gotcha', 'context', 'note', 'rejected', 'session'];
    const sections = order
      .filter(t => grouped[t]?.length)
      .map(t => {
        const header = `### ${t.charAt(0).toUpperCase() + t.slice(1)}s`;
        const items = grouped[t].map(e =>
          `- **${e.title}** [id:${e.id}]: ${e.content.split('\n')[0]}${e.tags.length ? ` *(${e.tags.join(', ')})*` : ''}`
        ).join('\n');
        return `${header}\n${items}`;
      });

    return sections.join('\n\n');
  }

  close(): void {
    this.db.close();
    this.globalDb?.close();
    this.workspaceDb?.close();
  }

  private deserialize(row: any): MemoryEntry {
    return {
      ...row,
      tags: JSON.parse(row.tags || '[]'),
      supersedes_id: row.supersedes_id ?? null,
      change_reason: row.change_reason ?? null,
    };
  }
}

export function resolveDbPath(projectRoot: string): string {
  return path.join(projectRoot, '.context', 'context.db');
}

export function resolveGlobalDbPath(): string {
  return path.join(os.homedir(), '.context-keeper', 'global.db');
}

const WORKSPACE_MARKERS = ['lerna.json', 'pnpm-workspace.yaml', 'nx.json', 'turbo.json'];

export function resolveWorkspaceRoot(projectRoot: string): string | null {
  let dir = path.dirname(projectRoot);
  for (let i = 0; i < 3; i++) {
    if (!dir || dir === projectRoot) return null;
    // manual sentinel file — drop a `.context-keeper-workspace` at your monorepo root
    if (fs.existsSync(path.join(dir, '.context-keeper-workspace'))) return dir;
    // standard monorepo markers
    for (const m of WORKSPACE_MARKERS) {
      if (fs.existsSync(path.join(dir, m))) return dir;
    }
    // package.json with "workspaces" field
    const pkgPath = path.join(dir, 'package.json');
    if (fs.existsSync(pkgPath)) {
      try {
        if (JSON.parse(fs.readFileSync(pkgPath, 'utf8')).workspaces) return dir;
      } catch { /* ignore parse errors */ }
    }
    const parent = path.dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
  return null;
}

export function resolveWorkspaceDbPath(projectRoot: string): string | null {
  const root = resolveWorkspaceRoot(projectRoot);
  return root ? path.join(root, '.context', 'workspace.db') : null;
}
