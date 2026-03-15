package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/context-keeper/context-keeper/internal/markdown"
	"github.com/context-keeper/context-keeper/internal/storage"
)

// Run dispatches to the appropriate hook handler.
func Run(hookName string, cfg storage.Config) error {
	store, err := storage.New(cfg)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	switch hookName {
	case "session-start":
		return sessionStart(store, cfg)
	case "stop":
		return stop(store, cfg)
	case "user-prompt":
		return userPrompt(store)
	case "post-tool-use":
		return postToolUse(store)
	case "exit-plan-mode":
		return exitPlanMode()
	default:
		return fmt.Errorf("unknown hook: %s", hookName)
	}
}

// ── session-start ─────────────────────────────────────────────────────────────

func sessionStart(store *storage.Storage, cfg storage.Config) error {
	md := markdown.GenerateContextMd(store.DB())
	wsMd := markdown.GenerateWorkspaceContextMd(store.WorkspaceDB(), cfg.ProjectRoot)

	// Write session start timestamp for stop hook
	ctxDir := filepath.Join(cfg.ProjectRoot, ".context")
	_ = os.MkdirAll(ctxDir, 0755)
	_ = os.WriteFile(filepath.Join(ctxDir, "session-start.tmp"), []byte(time.Now().UTC().Format("2006-01-02 15:04:05")), 0644)

	var sb strings.Builder
	sb.WriteString("# Project Memory (context-keeper)\n\n")

	if strings.HasPrefix(md, "<!-- empty -->") {
		sb.WriteString("No memory saved yet.\n\n")
		sb.WriteString("Action → type guide (save proactively without being asked):\n")
		sb.WriteString("- bug / fix / error → type: \"gotcha\"\n")
		sb.WriteString("- implement / add / build → type: \"decision\" or \"pattern\" (if reusable)\n")
		sb.WriteString("- tried & abandoned approach → type: \"rejected\"\n")
		sb.WriteString("- knowledge shared across all projects → type: \"workspace\"\n")
	} else {
		sb.WriteString("Compact index — use `get([id])` for full details.\n\n")
		sb.WriteString(md + "\n")
	}

	if wsMd != "" {
		sb.WriteString("\n## Workspace Memory (shared across projects)\n\n")
		sb.WriteString(wsMd + "\n")
	}

	fmt.Print(sb.String())
	return nil
}

// ── stop ──────────────────────────────────────────────────────────────────────

func stop(store *storage.Storage, cfg storage.Config) error {
	tmpFile := filepath.Join(cfg.ProjectRoot, ".context", "session-start.tmp")
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil // no session-start.tmp — nothing to summarise
	}
	_ = os.Remove(tmpFile)

	since := strings.TrimSpace(string(data))
	if since == "" {
		return nil
	}

	entries, err := store.ListSince(since)
	if err != nil {
		return err
	}

	// Filter out previous session entries
	var sessionEntries []storage.Entry
	for _, e := range entries {
		if e.Type != "session" {
			sessionEntries = append(sessionEntries, e)
		}
	}

	return store.AddSessionSummary(sessionEntries)
}

// ── user-prompt ───────────────────────────────────────────────────────────────

func userPrompt(store *storage.Storage) error {
	// Read stdin for the prompt text
	stdinData, _ := readStdin()
	prompt := extractPromptText(stdinData)

	if prompt == "" {
		return nil
	}

	// Detect what type of task this is and give hints
	hint := detectTypeHint(prompt)

	// Search for relevant memory
	localResults, _ := store.Search(prompt, 3)
	wsResults, _ := store.SearchWorkspace(prompt, 2)

	all := append(localResults, wsResults...)
	if len(all) > 4 {
		all = all[:4]
	}

	var sb strings.Builder

	if hint != "" {
		fmt.Fprintf(&sb, "[context-keeper: %s]\n", hint)
	}

	if len(all) > 0 {
		sb.WriteString("[context-keeper: relevant memory]\n")
		for _, r := range all {
			source := ""
			if r.Source != nil && *r.Source != "" {
				source = fmt.Sprintf(" [%s]", filepath.Base(*r.Source))
			}
			firstLine := r.Content
			if idx := strings.Index(firstLine, "\n"); idx != -1 {
				firstLine = firstLine[:idx]
			}
			if len(firstLine) > 80 {
				firstLine = firstLine[:80] + "…"
			}
			fmt.Fprintf(&sb, "- [%s] **%s** [id:%d]%s: %s\n", r.Type, r.Title, r.ID, source, firstLine)
		}
	}

	if sb.Len() > 0 {
		fmt.Print(sb.String())
	}
	return nil
}

// ── post-tool-use ─────────────────────────────────────────────────────────────

type toolUseInput struct {
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
}

func postToolUse(store *storage.Storage) error {
	stdinData, err := readStdin()
	if err != nil || len(stdinData) == 0 {
		return nil
	}

	var input toolUseInput
	if err := json.Unmarshal(stdinData, &input); err != nil {
		return nil
	}

	if input.ToolName != "Bash" && input.ToolName != "bash" {
		return nil
	}

	// Extract command, exit_code, stderr, stdout
	var toolInput struct {
		Command string `json:"command"`
	}
	var toolResponse struct {
		ExitCode int    `json:"exit_code"`
		Stderr   string `json:"stderr"`
		Stdout   string `json:"stdout"`
	}
	_ = json.Unmarshal(input.ToolInput, &toolInput)
	_ = json.Unmarshal(input.ToolResponse, &toolResponse)

	// Only capture failures
	if toolResponse.ExitCode == 0 && len(toolResponse.Stderr) <= 20 {
		return nil
	}

	errorText := toolResponse.Stderr
	if errorText == "" {
		errorText = toolResponse.Stdout
	}

	// Skip transient signals
	for _, skip := range []string{"interrupt", "killed", "SIGTERM", "SIGINT", "signal:"} {
		if strings.Contains(strings.ToLower(errorText), strings.ToLower(skip)) {
			return nil
		}
	}

	// Dedup: skip if similar entry already exists
	existing, _ := store.Search(errorText, 1)
	if len(existing) > 0 && existing[0].Score > 3 {
		return nil
	}

	// Truncate for title/content
	titleErr := errorText
	if len(titleErr) > 60 {
		titleErr = titleErr[:60]
	}
	cmd := toolInput.Command
	if len(cmd) > 120 {
		cmd = cmd[:120]
	}
	if len(errorText) > 500 {
		errorText = errorText[:500]
	}

	source := "post-tool-use-hook"
	_, _ = store.Add(storage.Entry{
		Type:    "gotcha",
		Title:   "[Auto] " + titleErr,
		Content: fmt.Sprintf("Command: %s\n\nError:\n%s", cmd, errorText),
		Tags:    []string{"auto-captured", "tool-failure"},
		Source:  &source,
	})
	return nil
}

// ── exit-plan-mode ────────────────────────────────────────────────────────────

func exitPlanMode() error {
	fmt.Print(`[context-keeper: Plan mode exited.
- If this plan was REJECTED → call remember(type="rejected", title="[Plan] ...", content="what was proposed + why you rejected it + what you chose instead")
- If this plan was ACCEPTED → it will be tracked via the implementation automatically]
`)
	return nil
}

// ── helpers ────────────────────────────────────────────────────────────────────

func readStdin() ([]byte, error) {
	info, err := os.Stdin.Stat()
	if err != nil || (info.Mode()&os.ModeCharDevice) != 0 {
		return nil, nil
	}
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

func extractPromptText(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	// Try JSON first
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
		for _, key := range []string{"prompt", "user_prompt", "message"} {
			if v, ok := obj[key].(string); ok {
				return v
			}
		}
	}
	return strings.TrimSpace(string(data))
}

func detectTypeHint(prompt string) string {
	lower := strings.ToLower(prompt)

	bugKeywords := []string{"fix", "bug", "broken", "error", "crash", "fail", "not working", "doesn't work", "issue"}
	for _, kw := range bugKeywords {
		if strings.Contains(lower, kw) {
			return "looks like a bug fix — save as type 'gotcha' when resolved"
		}
	}

	repeatKeywords := []string{"again", "same as", "repeated", "every time", "always", "keep having"}
	for _, kw := range repeatKeywords {
		if strings.Contains(lower, kw) {
			return "check patterns first with patterns() — you may have solved this before"
		}
	}

	buildKeywords := []string{"implement", "add", "create", "build", "integrate", "write"}
	for _, kw := range buildKeywords {
		if strings.Contains(lower, kw) {
			return "if this becomes reusable, save as type 'pattern' or 'decision'"
		}
	}

	refactorKeywords := []string{"refactor", "restructure", "rename", "migrate", "move"}
	for _, kw := range refactorKeywords {
		if strings.Contains(lower, kw) {
			return "architectural change — save as type 'decision'"
		}
	}

	return ""
}
