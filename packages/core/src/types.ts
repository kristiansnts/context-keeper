export interface MemoryEntry {
  id: number;
  type: 'decision' | 'convention' | 'gotcha' | 'context' | 'note';
  title: string;
  content: string;
  tags: string[];
  source?: string;
  created_at: string;
  updated_at: string;
}

export interface SearchResult extends MemoryEntry {
  score: number;
  snippet?: string;
}

export interface ContextKeeperConfig {
  dbPath: string;
  projectRoot: string;
}
