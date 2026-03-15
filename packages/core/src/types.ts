export interface MemoryEntry {
  id: number;
  type: 'decision' | 'convention' | 'gotcha' | 'context' | 'note' | 'session' | 'rejected' | 'pattern' | 'workspace';
  title: string;
  content: string;
  tags: string[];
  source?: string | null;
  /** ID of the entry this supersedes (for decision changes) */
  supersedes_id?: number | null;
  /** 'active' | 'superseded' — superseded entries are historical only */
  status: 'active' | 'superseded';
  /** Why this decision changed */
  change_reason?: string | null;
  created_at: string;
  updated_at: string;
}

export interface SearchResult extends MemoryEntry {
  score: number;
}

export interface DecisionHistory {
  current: MemoryEntry;
  history: MemoryEntry[];
}

export interface ContextKeeperConfig {
  dbPath: string;
  projectRoot: string;
  /** Optional path to global DB (~/.context-keeper/global.db) for cross-project search */
  globalDbPath?: string;
  /** Optional path to workspace DB (monorepo root/.context/workspace.db) for shared cross-project memory */
  workspaceDbPath?: string;
}
