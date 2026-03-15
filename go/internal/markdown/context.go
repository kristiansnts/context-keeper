package markdown

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// TypeOrder defines the display order of entry types in context output.
var TypeOrder = []string{
	"workspace", "decision", "pattern", "convention",
	"gotcha", "context", "note", "rejected", "session",
}

// WorkspaceTypeOrder is the subset shown in workspace context.
var WorkspaceTypeOrder = []string{"workspace", "decision", "pattern", "rejected"}

// TypeLabel returns the plural display label for a type.
func TypeLabel(t string) string {
	labels := map[string]string{
		"workspace":  "Workspace",
		"decision":   "Decisions",
		"pattern":    "Patterns",
		"convention": "Conventions",
		"gotcha":     "Gotchas",
		"context":    "Context",
		"note":       "Notes",
		"rejected":   "Rejected",
		"session":    "Sessions",
	}
	if l, ok := labels[t]; ok {
		return l
	}
	return strings.Title(t)
}

type rawEntry struct {
	id        int64
	typ       string
	title     string
	content   string
	tagsJSON  string
	source    sql.NullString
	status    string
	createdAt string
	updatedAt string
}

// GenerateContextMd produces a compact markdown index of all active entries.
func GenerateContextMd(db *sql.DB) string {
	entries, err := queryActive(db, "", 200)
	if err != nil || len(entries) == 0 {
		return "<!-- empty -->"
	}

	grouped := map[string][]rawEntry{}
	for _, e := range entries {
		grouped[e.typ] = append(grouped[e.typ], e)
	}

	var sb strings.Builder
	for _, typ := range TypeOrder {
		group, ok := grouped[typ]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "### %s\n", TypeLabel(typ))
		for _, e := range group {
			firstLine := firstLine(e.content)
			tags := parseTags(e.tagsJSON)
			line := fmt.Sprintf("- **%s** [id:%d]: %s", e.title, e.id, firstLine)
			if len(tags) > 0 {
				line += fmt.Sprintf(" *(%s)*", strings.Join(tags, ", "))
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// GenerateWorkspaceContextMd produces a compact markdown index of workspace entries.
// Returns empty string if the workspaceDb is nil or has no entries.
func GenerateWorkspaceContextMd(workspaceDb *sql.DB, projectRoot string) string {
	if workspaceDb == nil {
		return ""
	}

	entries, err := queryActive(workspaceDb, "", 200)
	if err != nil || len(entries) == 0 {
		return ""
	}

	grouped := map[string][]rawEntry{}
	for _, e := range entries {
		grouped[e.typ] = append(grouped[e.typ], e)
	}

	var sb strings.Builder
	for _, typ := range WorkspaceTypeOrder {
		group, ok := grouped[typ]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "### Workspace %s\n", TypeLabel(typ))
		for _, e := range group {
			firstLine := firstLine(e.content)
			projectName := "shared"
			if e.source.Valid && e.source.String != "" {
				projectName = filepath.Base(e.source.String)
			}
			line := fmt.Sprintf("- **%s** [id:%d] [%s]: %s", e.title, e.id, projectName, firstLine)
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// GenerateVerboseContextMd produces full markdown output for all entries, optionally filtered by type.
func GenerateVerboseContextMd(db *sql.DB, filterType string) string {
	entries, err := queryActive(db, filterType, 200)
	if err != nil || len(entries) == 0 {
		return "<!-- empty -->"
	}

	grouped := map[string][]rawEntry{}
	for _, e := range entries {
		grouped[e.typ] = append(grouped[e.typ], e)
	}

	order := TypeOrder
	if filterType != "" && filterType != "all" {
		order = []string{filterType}
	}

	var sb strings.Builder
	for _, typ := range order {
		group, ok := grouped[typ]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "## %s\n\n", TypeLabel(typ))
		for _, e := range group {
			tags := parseTags(e.tagsJSON)
			fmt.Fprintf(&sb, "### %s [id:%d]\n", e.title, e.id)
			if len(tags) > 0 {
				fmt.Fprintf(&sb, "*Tags: %s*\n\n", strings.Join(tags, ", "))
			}
			sb.WriteString(e.content + "\n\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func queryActive(db *sql.DB, typ string, limit int) ([]rawEntry, error) {
	var rows *sql.Rows
	var err error

	if typ == "" || typ == "all" {
		rows, err = db.Query(
			`SELECT id, type, title, content, tags, source, status, created_at, updated_at
			 FROM memory WHERE status = 'active' ORDER BY updated_at DESC LIMIT ?`, limit,
		)
	} else {
		rows, err = db.Query(
			`SELECT id, type, title, content, tags, source, status, created_at, updated_at
			 FROM memory WHERE status = 'active' AND type = ? ORDER BY updated_at DESC LIMIT ?`, typ, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []rawEntry
	for rows.Next() {
		var e rawEntry
		if err := rows.Scan(&e.id, &e.typ, &e.title, &e.content, &e.tagsJSON,
			&e.source, &e.status, &e.createdAt, &e.updatedAt); err != nil {
			continue
		}
		results = append(results, e)
	}
	return results, rows.Err()
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		s = s[:100] + "…"
	}
	return s
}

func parseTags(tagsJSON string) []string {
	var tags []string
	_ = json.Unmarshal([]byte(tagsJSON), &tags)
	return tags
}
