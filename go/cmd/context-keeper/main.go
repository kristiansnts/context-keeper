package main

import (
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/context-keeper/context-keeper/internal/dashboard"
	"github.com/context-keeper/context-keeper/internal/hooks"
	mcpserver "github.com/context-keeper/context-keeper/internal/mcp"
	"github.com/context-keeper/context-keeper/internal/storage"
)

func main() {
	projectRoot := os.Getenv("CONTEXT_KEEPER_ROOT")
	// If the env var is empty or still an unexpanded template (e.g. "${PROJECT_ROOT}"),
	// fall back to the current working directory.
	if projectRoot == "" || (len(projectRoot) > 2 && projectRoot[0] == '$' && projectRoot[1] == '{') {
		var err error
		projectRoot, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "context-keeper: failed to get working directory:", err)
			os.Exit(1)
		}
	}

	cfg := storage.Config{
		DbPath:          storage.ResolveDbPath(projectRoot),
		ProjectRoot:     projectRoot,
		GlobalDbPath:    storage.ResolveGlobalDbPath(),
		WorkspaceDbPath: storage.ResolveWorkspaceDbPath(projectRoot),
	}

	// Hook mode: invoked by Claude Code hooks
	if len(os.Args) >= 3 && os.Args[1] == "hook" {
		hookName := os.Args[2]
		if err := hooks.Run(hookName, cfg); err != nil {
			// Hooks are non-fatal — log to stderr but don't fail
			fmt.Fprintln(os.Stderr, "[context-keeper]", err)
		}
		return
	}

	// MCP server mode (default)
	s, err := mcpserver.NewServer(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "context-keeper: failed to start MCP server:", err)
		os.Exit(1)
	}

	// Start web dashboard in background
	port := os.Getenv("CONTEXT_KEEPER_PORT")
	dashboard.Start(s.Store(), port)

	if err := server.ServeStdio(s.MCP); err != nil {
		fmt.Fprintln(os.Stderr, "context-keeper: server error:", err)
		os.Exit(1)
	}
}
