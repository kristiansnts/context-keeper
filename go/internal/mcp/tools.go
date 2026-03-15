package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/context-keeper/context-keeper/internal/markdown"
	"github.com/context-keeper/context-keeper/internal/storage"
)

// Server wraps the MCP server and exposes the storage layer.
type Server struct {
	MCP   *server.MCPServer
	store *storage.Storage
}

// Store returns the underlying storage instance.
func (s *Server) Store() *storage.Storage { return s.store }

// NewServer creates and registers all context-keeper MCP tools.
func NewServer(cfg storage.Config) (*Server, error) {
	mcp := server.NewMCPServer(
		"context-keeper",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	store, err := storage.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}

	registerTools(mcp, store, cfg)
	return &Server{MCP: mcp, store: store}, nil
}

func registerTools(s *server.MCPServer, store *storage.Storage, cfg storage.Config) {
	// ── remember ──────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("remember",
		mcpgo.WithDescription("Proactively save a memory entry without being asked. "+
			"Action → type guide:\n"+
			"- fix/bug/error/crash → type: gotcha\n"+
			"- implement/add/build/integrate → type: pattern (if reusable) or decision (if architectural)\n"+
			"- refactor/restructure/migrate → type: decision\n"+
			"- tried & abandoned → type: rejected\n"+
			"- knowledge shared across all projects → type: workspace\n"+
			"- coding convention for this project → type: convention"),
		mcpgo.WithString("type",
			mcpgo.Required(),
			mcpgo.Enum("decision", "convention", "gotcha", "context", "note", "rejected", "pattern", "workspace", "session"),
			mcpgo.Description("The type of memory entry")),
		mcpgo.WithString("title",
			mcpgo.Required(),
			mcpgo.Description("Short descriptive title, max 80 chars")),
		mcpgo.WithString("content",
			mcpgo.Required(),
			mcpgo.Description("Full explanation including reasoning")),
		mcpgo.WithArray("tags",
			mcpgo.Description("Optional tags for categorization")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		typ, _ := req.GetArguments()["type"].(string)
		title, _ := req.GetArguments()["title"].(string)
		content, _ := req.GetArguments()["content"].(string)
		tags := parseTags(req.GetArguments()["tags"])

		entry, err := store.Add(storage.Entry{
			Type:    typ,
			Title:   title,
			Content: content,
			Tags:    tags,
		})
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("✅ Saved [%s] %s [id:%d]", entry.Type, entry.Title, entry.ID)), nil
	})

	// ── update_decision ────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("update_decision",
		mcpgo.WithDescription("Update an existing decision when it changes. Preserves old version as history."),
		mcpgo.WithNumber("id",
			mcpgo.Required(),
			mcpgo.Description("ID of the existing decision to update (from [id:N])")),
		mcpgo.WithString("title",
			mcpgo.Required(),
			mcpgo.Description("New title")),
		mcpgo.WithString("content",
			mcpgo.Required(),
			mcpgo.Description("New content explaining the updated decision")),
		mcpgo.WithString("why_changed",
			mcpgo.Required(),
			mcpgo.Description("CRITICAL — Why the decision changed")),
		mcpgo.WithArray("tags",
			mcpgo.Description("Tags for the new version")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		id := int64(req.GetArguments()["id"].(float64))
		title, _ := req.GetArguments()["title"].(string)
		content, _ := req.GetArguments()["content"].(string)
		whyChanged, _ := req.GetArguments()["why_changed"].(string)
		tags := parseTags(req.GetArguments()["tags"])

		newEntry, err := store.UpdateDecision(id, title, content, tags, whyChanged)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf(
			"✅ Updated decision [id:%d] — previous version preserved as history [id:%d]",
			newEntry.ID, id,
		)), nil
	})

	// ── search ─────────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("search",
		mcpgo.WithDescription("Search project memory using natural language. Call before starting new tasks."),
		mcpgo.WithString("query",
			mcpgo.Required(),
			mcpgo.Description("Natural language search query")),
		mcpgo.WithNumber("limit",
			mcpgo.Description("Max results, default 5")),
		mcpgo.WithString("scope",
			mcpgo.Enum("local", "workspace", "global", "all"),
			mcpgo.Description("Search scope: local (default), workspace, global, all")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		query, _ := req.GetArguments()["query"].(string)
		limit := 5
		if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		scope, _ := req.GetArguments()["scope"].(string)
		if scope == "" {
			scope = "local"
		}

		var results []storage.SearchResult
		var err error
		switch scope {
		case "global":
			results, err = store.SearchGlobal(query, limit)
		case "workspace":
			results, err = store.SearchWorkspace(query, limit)
		case "all":
			results, err = store.SearchAll(query, limit)
		default:
			results, err = store.Search(query, limit)
		}
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		if len(results) == 0 {
			return mcpgo.NewToolResultText("No results found."), nil
		}

		var sb strings.Builder
		for _, r := range results {
			source := ""
			if r.Source != nil && *r.Source != "" && *r.Source != cfg.ProjectRoot {
				source = fmt.Sprintf(" [%s]", filepath.Base(*r.Source))
			}
			tags := ""
			if len(r.Tags) > 0 {
				tags = fmt.Sprintf(" *(%s)*", strings.Join(r.Tags, ", "))
			}
			fmt.Fprintf(&sb, "### [%s] %s [id:%d]%s%s\n%s\n\n",
				r.Type, r.Title, r.ID, source, tags, r.Content)
		}
		return mcpgo.NewToolResultText(sb.String()), nil
	})

	// ── context ────────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("context",
		mcpgo.WithDescription("Load project memory. Returns compact index by default (token-efficient). "+
			"Prefer calling context() then get([id]) for full details."),
		mcpgo.WithString("type",
			mcpgo.Enum("decision", "convention", "gotcha", "context", "note", "rejected", "pattern", "workspace", "session", "all"),
			mcpgo.Description("Filter by type, default 'all'")),
		mcpgo.WithBoolean("verbose",
			mcpgo.Description("If true, return full content. Default false (compact index).")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		typ, _ := req.GetArguments()["type"].(string)
		verbose, _ := req.GetArguments()["verbose"].(bool)

		db := store.DB()
		var out string
		if verbose {
			out = markdown.GenerateVerboseContextMd(db, typ)
		} else {
			out = markdown.GenerateContextMd(db)
		}
		return mcpgo.NewToolResultText(out), nil
	})

	// ── get ────────────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("get",
		mcpgo.WithDescription("Fetch full content for one or more memory entries by ID."),
		mcpgo.WithArray("ids",
			mcpgo.Required(),
			mcpgo.Description("Array of entry IDs to fetch")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		rawIDs, _ := req.GetArguments()["ids"].([]interface{})
		ids := make([]int64, 0, len(rawIDs))
		for _, v := range rawIDs {
			switch val := v.(type) {
			case float64:
				ids = append(ids, int64(val))
			case json.Number:
				n, _ := val.Int64()
				ids = append(ids, n)
			}
		}

		entries, err := store.GetByIDs(ids)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if len(entries) == 0 {
			return mcpgo.NewToolResultText("No entries found."), nil
		}

		var sb strings.Builder
		for _, e := range entries {
			tags := ""
			if len(e.Tags) > 0 {
				tags = fmt.Sprintf("\n*Tags: %s*", strings.Join(e.Tags, ", "))
			}
			fmt.Fprintf(&sb, "## [%s] %s [id:%d]%s\n\n%s\n\n", e.Type, e.Title, e.ID, tags, e.Content)
		}
		return mcpgo.NewToolResultText(sb.String()), nil
	})

	// ── history ────────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("history",
		mcpgo.WithDescription("Get the full change history of a decision — current + all superseded ancestors."),
		mcpgo.WithNumber("id",
			mcpgo.Required(),
			mcpgo.Description("ID of the decision to get history for")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		id := int64(req.GetArguments()["id"].(float64))

		hist, err := store.GetHistory(id)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "## %s [id:%d] (current)\n\n%s\n\n", hist.Current.Title, hist.Current.ID, hist.Current.Content)

		for i, prev := range hist.History {
			version := len(hist.History) - i
			reason := ""
			if hist.History[i-1].ChangeReason != nil {
				reason = fmt.Sprintf("\n*Changed because: %s*", *hist.History[i-1].ChangeReason)
			}
			fmt.Fprintf(&sb, "---\n### v%d [id:%d] (superseded %s)%s\n\n%s\n\n",
				version, prev.ID, prev.UpdatedAt[:10], reason, prev.Content)
		}
		return mcpgo.NewToolResultText(sb.String()), nil
	})

	// ── decisions ─────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("decisions",
		mcpgo.WithDescription("List all architectural decisions. Use when asked about architecture or technology choices."),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		entries, err := store.ListByType("decision")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if len(entries) == 0 {
			return mcpgo.NewToolResultText("No decisions saved yet."), nil
		}
		return mcpgo.NewToolResultText(formatEntryList(entries)), nil
	})

	// ── conventions ────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("conventions",
		mcpgo.WithDescription("Get all coding conventions. Call before writing code to ensure you follow established patterns."),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		entries, err := store.ListByType("convention")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if len(entries) == 0 {
			return mcpgo.NewToolResultText("No conventions saved yet."), nil
		}
		return mcpgo.NewToolResultText(formatEntryList(entries)), nil
	})

	// ── patterns ───────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("patterns",
		mcpgo.WithDescription("List saved solution patterns. Call FIRST when asked to do something you may have done before."),
		mcpgo.WithString("search",
			mcpgo.Description("Keyword to filter patterns")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		searchQuery, _ := req.GetArguments()["search"].(string)

		if searchQuery != "" {
			results, err := store.Search(searchQuery, 20)
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			// Filter to patterns only
			var entries []storage.Entry
			for _, r := range results {
				if r.Type == "pattern" {
					entries = append(entries, r.Entry)
				}
			}
			if len(entries) == 0 {
				return mcpgo.NewToolResultText("No matching patterns found."), nil
			}
			return mcpgo.NewToolResultText(formatEntryList(entries)), nil
		}

		entries, err := store.ListByType("pattern")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if len(entries) == 0 {
			return mcpgo.NewToolResultText("No patterns saved yet."), nil
		}
		return mcpgo.NewToolResultText(formatEntryList(entries)), nil
	})
}

// ── helpers ────────────────────────────────────────────────────────────────────

func parseTags(v interface{}) []string {
	if v == nil {
		return []string{}
	}
	arr, ok := v.([]interface{})
	if !ok {
		return []string{}
	}
	tags := make([]string, 0, len(arr))
	for _, t := range arr {
		if s, ok := t.(string); ok {
			tags = append(tags, s)
		}
	}
	return tags
}

func formatEntryList(entries []storage.Entry) string {
	var sb strings.Builder
	for _, e := range entries {
		tags := ""
		if len(e.Tags) > 0 {
			tags = fmt.Sprintf("\n*Tags: %s*", strings.Join(e.Tags, ", "))
		}
		fmt.Fprintf(&sb, "## %s [id:%d]%s\n\n%s\n\n", e.Title, e.ID, tags, e.Content)
	}
	return strings.TrimRight(sb.String(), "\n")
}
