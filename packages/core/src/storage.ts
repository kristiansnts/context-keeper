import Database from 'better-sqlite3';
import path from 'path';
import fs from 'fs';
import type { MemoryEntry, SearchResult, ContextKeeperConfig } from './types';

const SCHEMA = `
  CREATE TABLE IF NOT EXISTS memory (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT NOT NULL DEFAULT 'note',
    title       TEXT NOT NULL,
    content     TEXT NOT NULL,
    tags        TEXT NOT NULL DEFAULT '[]',
    source      TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
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

export class ContextKeeperStorage {
  private db: Database.Database;

  constructor(config: ContextKeeperConfig) {
    const dir = path.dirname(config.dbPath);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    this.db = new Database(config.dbPath);
    this.db.pragma('journal_mode = WAL');
    this.db.exec(SCHEMA);
  }

  add(entry: Omit<MemoryEntry, 'id' | 'created_at' | 'updated_at'>): MemoryEntry {
    const stmt = this.db.prepare(`
      INSERT INTO memory (type, title, content, tags, source)
      VALUES (@type, @title, @content, @tags, @source)
    `);
    const result = stmt.run({
      ...entry,
      tags: JSON.stringify(entry.tags),
      source: entry.source ?? null,
    });
    return this.getById(result.lastInsertRowid as number)!;
  }

  getById(id: number): MemoryEntry | undefined {
    const row = this.db.prepare('SELECT * FROM memory WHERE id = ?').get(id) as any;
    return row ? this.deserialize(row) : undefined;
  }

  search(query: string, limit = 10): SearchResult[] {
    const sanitized = query.replace(/['"*]/g, ' ').trim();
    if (!sanitized) return this.list(limit).map(e => ({ ...e, score: 1 }));

    const rows = this.db.prepare(`
      SELECT m.*, memory_fts.rank AS score
      FROM memory_fts
      JOIN memory m ON memory_fts.rowid = m.id
      WHERE memory_fts MATCH ?
      ORDER BY rank
      LIMIT ?
    `).all(`${sanitized}*`, limit) as any[];

    return rows.map(row => ({
      ...this.deserialize(row),
      score: Math.abs(row.score),
    }));
  }

  list(limit = 50, type?: MemoryEntry['type']): MemoryEntry[] {
    let stmt;
    if (type) {
      stmt = this.db.prepare('SELECT * FROM memory WHERE type = ? ORDER BY updated_at DESC LIMIT ?').all(type, limit);
    } else {
      stmt = this.db.prepare('SELECT * FROM memory ORDER BY updated_at DESC LIMIT ?').all(limit);
    }
    return (stmt as any[]).map(row => this.deserialize(row));
  }

  delete(id: number): boolean {
    const result = this.db.prepare('DELETE FROM memory WHERE id = ?').run(id);
    return result.changes > 0;
  }

  summary(): string {
    const counts = this.db.prepare(`
      SELECT type, COUNT(*) as count FROM memory GROUP BY type
    `).all() as { type: string; count: number }[];

    if (counts.length === 0) return 'No project memory yet. Run `ctx add` to start building context.';

    const lines = counts.map(r => `  ${r.type}: ${r.count} entries`);
    const total = counts.reduce((s, r) => s + r.count, 0);
    return `Project Memory Summary (${total} total):\n${lines.join('\n')}`;
  }

  close(): void {
    this.db.close();
  }

  private deserialize(row: any): MemoryEntry {
    return {
      ...row,
      tags: JSON.parse(row.tags || '[]'),
    };
  }
}

export function resolveDbPath(projectRoot: string): string {
  return path.join(projectRoot, '.context', 'context.db');
}
