package storage

const schema = `
CREATE TABLE IF NOT EXISTS memory (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  type             TEXT NOT NULL DEFAULT 'note',
  title            TEXT NOT NULL,
  content          TEXT NOT NULL,
  tags             TEXT NOT NULL DEFAULT '[]',
  source           TEXT,
  status           TEXT NOT NULL DEFAULT 'active',
  supersedes_id    INTEGER REFERENCES memory(id),
  change_reason    TEXT,
  created_at       TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at       TEXT NOT NULL DEFAULT (datetime('now')),
  staleness_risk   TEXT NOT NULL DEFAULT 'low',
  last_verified_at TEXT NOT NULL DEFAULT (datetime('now'))
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
`

var migrations = []string{
	`ALTER TABLE memory ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`,
	`ALTER TABLE memory ADD COLUMN supersedes_id INTEGER`,
	`ALTER TABLE memory ADD COLUMN change_reason TEXT`,
	`ALTER TABLE memory ADD COLUMN staleness_risk TEXT NOT NULL DEFAULT 'low'`,
	`ALTER TABLE memory ADD COLUMN last_verified_at TEXT NOT NULL DEFAULT ''`,
}
