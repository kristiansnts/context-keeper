package storage

import (
	"os"
	"path/filepath"
)

// ResolveDbPath returns <projectRoot>/.context/context.db
func ResolveDbPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".context", "context.db")
}

// ResolveGlobalDbPath returns ~/.context-keeper/global.db
func ResolveGlobalDbPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".context-keeper", "global.db")
}

// ResolveWorkspaceRoot walks up from projectRoot (up to 3 levels) looking for
// monorepo markers. Returns the workspace root or empty string if not found.
func ResolveWorkspaceRoot(projectRoot string) string {
	monorepoMarkers := []string{
		"lerna.json", "pnpm-workspace.yaml", "nx.json", "turbo.json",
		".context-keeper-workspace",
	}

	dir := projectRoot
	for i := 0; i < 3; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent

		// Check for sentinel file
		for _, marker := range monorepoMarkers {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}

		// Check for package.json with "workspaces" field
		if hasWorkspacesField(filepath.Join(dir, "package.json")) {
			return dir
		}
	}
	return ""
}

// ResolveWorkspaceDbPath returns <workspaceRoot>/.context/workspace.db or empty string.
func ResolveWorkspaceDbPath(projectRoot string) string {
	root := ResolveWorkspaceRoot(projectRoot)
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".context", "workspace.db")
}

func hasWorkspacesField(pkgJsonPath string) bool {
	data, err := os.ReadFile(pkgJsonPath)
	if err != nil {
		return false
	}
	// Quick string check — avoids importing encoding/json for a hot path
	content := string(data)
	return contains(content, `"workspaces"`)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
