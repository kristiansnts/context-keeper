package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// ValidTypes lists all allowed memory entry types.
var ValidTypes = []string{
	"decision", "convention", "gotcha", "context",
	"note", "session", "rejected", "pattern", "workspace",
}

// WorkspaceMirrorTypes are mirrored to workspace/global DBs.
var WorkspaceMirrorTypes = map[string]bool{
	"decision": true, "pattern": true, "workspace": true, "rejected": true,
}

// Entry represents a memory record.
type Entry struct {
	ID           int64
	Type         string
	Title        string
	Content      string
	Tags         []string
	Source       *string
	Status       string
	SupersedesID *int64
	ChangeReason *string
	CreatedAt    string
	UpdatedAt    string
}

// SearchResult wraps Entry with a relevance score.
type SearchResult struct {
	Entry
	Score float64
}

// DecisionHistory contains the current version and all superseded ancestors.
type DecisionHistory struct {
	Current Entry
	History []Entry
}

// Config holds database paths and project root.
type Config struct {
	DbPath        string
	ProjectRoot   string
	GlobalDbPath  string
	WorkspaceDbPath string
}

// Storage manages SQLite databases for a project.
type Storage struct {
	cfg         Config
	db          *sql.DB
	globalDb    *sql.DB
	workspaceDb *sql.DB
	onAdd       func(Entry)
}

// OnAdd registers a callback invoked after every successful Add.
func (s *Storage) OnAdd(fn func(Entry)) { s.onAdd = fn }

// New opens (and initialises) all configured databases.
func New(cfg Config) (*Storage, error) {
	s := &Storage{cfg: cfg}

	db, err := openDB(cfg.DbPath)
	if err != nil {
		return nil, fmt.Errorf("open project db: %w", err)
	}
	s.db = db

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate project db: %w", err)
	}

	if cfg.GlobalDbPath != "" {
		gdb, err := openDB(cfg.GlobalDbPath)
		if err == nil {
			s.globalDb = gdb
			_ = migrate(gdb)
		}
	}

	if cfg.WorkspaceDbPath != "" {
		wdb, err := openDB(cfg.WorkspaceDbPath)
		if err == nil {
			s.workspaceDb = wdb
			_ = migrate(wdb)
		}
	}

	return s, nil
}

// DB returns the underlying project database connection (for markdown generation).
func (s *Storage) DB() *sql.DB {
	return s.db
}

// WorkspaceDB returns the workspace database connection, or nil.
func (s *Storage) WorkspaceDB() *sql.DB {
	return s.workspaceDb
}

// Close closes all open database connections.
func (s *Storage) Close() {
	if s.db != nil {
		s.db.Close()
	}
	if s.globalDb != nil {
		s.globalDb.Close()
	}
	if s.workspaceDb != nil {
		s.workspaceDb.Close()
	}
}

// Add inserts a new entry and mirrors it to global/workspace DBs.
func (s *Storage) Add(e Entry) (Entry, error) {
	id, err := insertEntry(s.db, e)
	if err != nil {
		return Entry{}, err
	}
	result, err := s.GetByID(id)
	if err != nil {
		return Entry{}, err
	}

	// Mirror to global DB (tag with project root as source)
	if s.globalDb != nil {
		mirrored := e
		mirrored.Source = &s.cfg.ProjectRoot
		_, _ = insertEntry(s.globalDb, mirrored)
	}

	// Mirror workspace-relevant types to workspace DB
	if s.workspaceDb != nil && WorkspaceMirrorTypes[e.Type] {
		mirrored := e
		mirrored.Source = &s.cfg.ProjectRoot
		_, _ = insertEntry(s.workspaceDb, mirrored)
	}

	if s.onAdd != nil {
		s.onAdd(result)
	}
	return result, nil
}

// UpdateDecision marks the old entry superseded and creates a new active entry.
func (s *Storage) UpdateDecision(supersedesID int64, title, content string, tags []string, changeReason string) (Entry, error) {
	old, err := s.GetByID(supersedesID)
	if err != nil {
		return Entry{}, fmt.Errorf("entry #%d not found: %w", supersedesID, err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE memory SET status = 'superseded', updated_at = datetime('now') WHERE id = ?`, supersedesID)
	if err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, err
	}

	sid := supersedesID
	newEntry := Entry{
		Type:         old.Type,
		Title:        title,
		Content:      content,
		Tags:         tags,
		SupersedesID: &sid,
		ChangeReason: &changeReason,
	}
	return s.Add(newEntry)
}

// GetByID fetches a single entry by ID.
func (s *Storage) GetByID(id int64) (Entry, error) {
	row := s.db.QueryRow(`SELECT * FROM memory WHERE id = ?`, id)
	return scanEntry(row)
}

// GetByIDs fetches multiple entries by ID.
func (s *Storage) GetByIDs(ids []int64) ([]Entry, error) {
	results := make([]Entry, 0, len(ids))
	for _, id := range ids {
		e, err := s.GetByID(id)
		if err != nil {
			continue
		}
		results = append(results, e)
	}
	return results, nil
}

// List returns up to limit active entries.
func (s *Storage) List(limit int) ([]Entry, error) {
	rows, err := s.db.Query(`SELECT * FROM memory WHERE status = 'active' ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// ListSince returns active entries created after the given datetime string.
func (s *Storage) ListSince(since string) ([]Entry, error) {
	rows, err := s.db.Query(
		`SELECT * FROM memory WHERE status = 'active' AND created_at >= ? ORDER BY created_at ASC`,
		since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// ListByType returns all active entries of a given type.
func (s *Storage) ListByType(typ string) ([]Entry, error) {
	rows, err := s.db.Query(
		`SELECT * FROM memory WHERE status = 'active' AND type = ? ORDER BY updated_at DESC`,
		typ,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// ListAll returns all active entries, optionally filtered by type.
func (s *Storage) ListAll(typ string) ([]Entry, error) {
	if typ == "" || typ == "all" {
		return s.List(200)
	}
	return s.ListByType(typ)
}

// ListProjects returns distinct project names (basename of source) from the global DB.
func (s *Storage) ListProjects() []string {
	if s.globalDb == nil {
		return nil
	}
	rows, err := s.globalDb.Query(
		`SELECT DISTINCT source FROM memory WHERE status='active' AND source IS NOT NULL AND source != '' ORDER BY source`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var projects []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err == nil && src != "" {
			projects = append(projects, src)
		}
	}
	return projects
}

// ListGlobalByProject returns all active entries for a given project source path.
// If source is empty, returns all entries across all projects from the global DB.
func (s *Storage) ListGlobalByProject(source, typ string) ([]Entry, error) {
	if s.globalDb == nil {
		return s.ListAll(typ)
	}
	var (
		args  []interface{}
		where = "status='active'"
	)
	if source != "" {
		where += " AND source=?"
		args = append(args, source)
	}
	if typ != "" && typ != "all" {
		where += " AND type=?"
		args = append(args, typ)
	}
	rows, err := s.globalDb.Query(
		`SELECT id,type,title,content,tags,source,status,supersedes_id,change_reason,created_at,updated_at
		 FROM memory WHERE `+where+` ORDER BY updated_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// GetHistory returns the full decision history chain for an entry.
func (s *Storage) GetHistory(id int64) (*DecisionHistory, error) {
	current, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}

	var history []Entry
	cur := current
	for cur.SupersedesID != nil {
		prev, err := s.GetByID(*cur.SupersedesID)
		if err != nil {
			break
		}
		history = append(history, prev)
		cur = prev
	}

	return &DecisionHistory{Current: current, History: history}, nil
}

// Search runs FTS5 search on the local DB.
func (s *Storage) Search(query string, limit int) ([]SearchResult, error) {
	return ftsSearch(s.db, query, limit, "", "")
}

// SearchGlobal searches the global DB excluding entries from the current project.
func (s *Storage) SearchGlobal(query string, limit int) ([]SearchResult, error) {
	if s.globalDb == nil {
		return nil, nil
	}
	return ftsSearch(s.globalDb, query, limit, "exclude_source", s.cfg.ProjectRoot)
}

// SearchWorkspace searches the workspace DB.
func (s *Storage) SearchWorkspace(query string, limit int) ([]SearchResult, error) {
	if s.workspaceDb == nil {
		return nil, nil
	}
	return ftsSearch(s.workspaceDb, query, limit, "", "")
}

// SearchAll merges results from local, workspace, and global DBs.
func (s *Storage) SearchAll(query string, limit int) ([]SearchResult, error) {
	local, _ := s.Search(query, limit)
	workspace, _ := s.SearchWorkspace(query, limit)
	global, _ := s.SearchGlobal(query, limit)

	merged := append(local, workspace...)
	merged = append(merged, global...)

	// Sort by score descending
	for i := 1; i < len(merged); i++ {
		for j := i; j > 0 && merged[j].Score > merged[j-1].Score; j-- {
			merged[j], merged[j-1] = merged[j-1], merged[j]
		}
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// AddSessionSummary saves a session-type entry summarising entries created this session.
func (s *Storage) AddSessionSummary(entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d entries saved this session:\n", len(entries))
	for _, e := range entries {
		firstLine := e.Content
		if idx := strings.Index(firstLine, "\n"); idx != -1 {
			firstLine = firstLine[:idx]
		}
		if len(firstLine) > 80 {
			firstLine = firstLine[:80] + "…"
		}
		fmt.Fprintf(&sb, "- [%s] %s: %s\n", e.Type, e.Title, firstLine)
	}

	source := "stop-hook"
	_, err := s.Add(Entry{
		Type:    "session",
		Title:   fmt.Sprintf("Session %s", entries[0].CreatedAt[:16]),
		Content: sb.String(),
		Tags:    []string{"auto-saved"},
		Source:  &source,
	})
	return err
}

// ── helpers ────────────────────────────────────────────────────────────────────

func openDB(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite concurrency: WAL allows concurrent reads but one writer
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(memory)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ, notnull, dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		existing[name.String] = true
	}

	needed := map[string]string{
		"status":        `ALTER TABLE memory ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`,
		"supersedes_id": `ALTER TABLE memory ADD COLUMN supersedes_id INTEGER`,
		"change_reason": `ALTER TABLE memory ADD COLUMN change_reason TEXT`,
	}
	for col, stmt := range needed {
		if !existing[col] {
			if _, err := db.Exec(stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertEntry(db *sql.DB, e Entry) (int64, error) {
	tagsJSON, _ := json.Marshal(e.Tags)
	res, err := db.Exec(
		`INSERT INTO memory (type, title, content, tags, source, supersedes_id, change_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Type, e.Title, e.Content, string(tagsJSON), e.Source, e.SupersedesID, e.ChangeReason,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func ftsSearch(db *sql.DB, query string, limit int, mode, modeArg string) ([]SearchResult, error) {
	ftsQuery := sanitizeQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	var rows *sql.Rows
	var err error

	if mode == "exclude_source" {
		rows, err = db.Query(
			`SELECT m.*, memory_fts.rank AS score
			 FROM memory_fts
			 JOIN memory m ON memory_fts.rowid = m.id
			 WHERE memory_fts MATCH ?
			   AND m.status = 'active'
			   AND (m.source IS NULL OR m.source != ?)
			 ORDER BY rank LIMIT ?`,
			ftsQuery, modeArg, limit,
		)
	} else {
		rows, err = db.Query(
			`SELECT m.*, memory_fts.rank AS score
			 FROM memory_fts
			 JOIN memory m ON memory_fts.rowid = m.id
			 WHERE memory_fts MATCH ?
			   AND m.status = 'active'
			 ORDER BY rank LIMIT ?`,
			ftsQuery, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var score float64
		var e Entry
		var tagsStr string
		var source, changeReason sql.NullString
		var supersedesID sql.NullInt64

		err := rows.Scan(
			&e.ID, &e.Type, &e.Title, &e.Content, &tagsStr,
			&source, &e.Status, &supersedesID, &changeReason,
			&e.CreatedAt, &e.UpdatedAt, &score,
		)
		if err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(tagsStr), &e.Tags)
		if source.Valid {
			e.Source = &source.String
		}
		if supersedesID.Valid {
			e.SupersedesID = &supersedesID.Int64
		}
		if changeReason.Valid {
			e.ChangeReason = &changeReason.String
		}
		if score < 0 {
			score = -score
		}
		results = append(results, SearchResult{Entry: e, Score: score})
	}
	return results, nil
}

func scanEntry(row *sql.Row) (Entry, error) {
	var e Entry
	var tagsStr string
	var source, changeReason sql.NullString
	var supersedesID sql.NullInt64

	err := row.Scan(
		&e.ID, &e.Type, &e.Title, &e.Content, &tagsStr,
		&source, &e.Status, &supersedesID, &changeReason,
		&e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return Entry{}, err
	}
	_ = json.Unmarshal([]byte(tagsStr), &e.Tags)
	if source.Valid {
		e.Source = &source.String
	}
	if supersedesID.Valid {
		e.SupersedesID = &supersedesID.Int64
	}
	if changeReason.Valid {
		e.ChangeReason = &changeReason.String
	}
	return e, nil
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var results []Entry
	for rows.Next() {
		var e Entry
		var tagsStr string
		var source, changeReason sql.NullString
		var supersedesID sql.NullInt64

		err := rows.Scan(
			&e.ID, &e.Type, &e.Title, &e.Content, &tagsStr,
			&source, &e.Status, &supersedesID, &changeReason,
			&e.CreatedAt, &e.UpdatedAt,
		)
		if err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(tagsStr), &e.Tags)
		if source.Valid {
			e.Source = &source.String
		}
		if supersedesID.Valid {
			e.SupersedesID = &supersedesID.Int64
		}
		if changeReason.Valid {
			e.ChangeReason = &changeReason.String
		}
		results = append(results, e)
	}
	return results, rows.Err()
}

// sanitizeQuery converts a natural language query into an FTS5 OR-prefix query.
// "storage backend database" → "storage* OR backend* OR database*"
// This ensures any matching word returns results, not just entries containing all words.
func sanitizeQuery(q string) string {
	q = strings.ReplaceAll(q, "'", " ")
	q = strings.ReplaceAll(q, `"`, " ")
	q = strings.ReplaceAll(q, "*", " ")
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	words := strings.Fields(q)
	terms := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) >= 2 { // skip single-char words
			terms = append(terms, w+"*")
		}
	}
	if len(terms) == 0 {
		return ""
	}
	return strings.Join(terms, " OR ")
}
