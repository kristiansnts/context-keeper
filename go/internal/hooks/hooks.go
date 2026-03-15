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
		return postToolUse(store, cfg)
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

	obsFile := filepath.Join(cfg.ProjectRoot, ".context", "session-obs.jsonl")
	obs := readObservations(obsFile)
	_ = os.Remove(obsFile)

	gotchaCount := 0
	for _, e := range sessionEntries {
		if e.Type == "gotcha" {
			gotchaCount++
		}
	}
	obsContent := buildSessionSummaryFromObs(obs, gotchaCount)

	return store.AddSessionSummary(sessionEntries, obsContent)
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

type rawObservation struct {
	Tool string `json:"tool"`
	File string `json:"file,omitempty"`
	Cmd  string `json:"cmd,omitempty"`
	Exit *int   `json:"exit,omitempty"`
	Ts   string `json:"ts"`
}

func isNoisyTool(name string) bool {
	switch name {
	case "Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch", "Agent", "TodoRead", "TodoWrite":
		return true
	}
	return false
}

func isNoisyBashCommand(cmd string) bool {
	noisy := []string{"ls", "echo", "cat", "pwd", "which", "head", "tail", "grep", "find", "sed", "awk", "rg", "wc"}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}
	word := strings.ToLower(fields[0])
	for _, n := range noisy {
		if word == n {
			return true
		}
	}
	return false
}

func appendObservation(cfg storage.Config, obs rawObservation) error {
	obs.Ts = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(obs)
	if err != nil {
		return err
	}
	ctxDir := filepath.Join(cfg.ProjectRoot, ".context")
	_ = os.MkdirAll(ctxDir, 0755)
	f, err := os.OpenFile(filepath.Join(ctxDir, "session-obs.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func readObservations(path string) []rawObservation {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var results []rawObservation
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obs rawObservation
		if err := json.Unmarshal([]byte(line), &obs); err == nil {
			results = append(results, obs)
		}
	}
	return results
}

func buildSessionSummaryFromObs(obs []rawObservation, gotchaCount int) string {
	var filesOrder []string
	filesSeen := map[string]bool{}
	var cmdsOrder []string
	cmdsSeen := map[string]bool{}

	for _, o := range obs {
		switch o.Tool {
		case "Edit", "Write", "NotebookEdit":
			if o.File != "" && !filesSeen[o.File] {
				filesSeen[o.File] = true
				filesOrder = append(filesOrder, filepath.Base(o.File))
			}
		case "Bash":
			if o.Cmd != "" && !cmdsSeen[o.Cmd] {
				cmdsSeen[o.Cmd] = true
				cmdsOrder = append(cmdsOrder, o.Cmd)
			}
		}
	}

	var sb strings.Builder
	if len(filesOrder) > 0 {
		fmt.Fprintf(&sb, "Files changed (%d): %s\n", len(filesOrder), strings.Join(filesOrder, ", "))
	}
	if len(cmdsOrder) > 0 {
		fmt.Fprintf(&sb, "Commands run (%d): %s\n", len(cmdsOrder), strings.Join(cmdsOrder, ", "))
	}
	if gotchaCount > 0 {
		fmt.Fprintf(&sb, "Bash failures (%d): auto-captured as gotcha entries\n", gotchaCount)
	}
	return sb.String()
}

func postToolUse(store *storage.Storage, cfg storage.Config) error {
	stdinData, err := readStdin()
	if err != nil || len(stdinData) == 0 {
		return nil
	}

	var input toolUseInput
	if err := json.Unmarshal(stdinData, &input); err != nil {
		return nil
	}

	toolName := input.ToolName

	// Bash path: handle failures and success separately
	if strings.EqualFold(toolName, "Bash") {
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

		// Failure path (existing logic)
		if toolResponse.ExitCode != 0 || len(toolResponse.Stderr) > 20 {
			errorText := toolResponse.Stderr
			if errorText == "" {
				errorText = toolResponse.Stdout
			}
			for _, skip := range []string{"interrupt", "killed", "SIGTERM", "SIGINT", "signal:"} {
				if strings.Contains(strings.ToLower(errorText), strings.ToLower(skip)) {
					return nil
				}
			}
			existing, _ := store.Search(errorText, 1)
			if len(existing) > 0 && existing[0].Score > 3 {
				return nil
			}
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

		// Success path: append observation if not noisy
		if toolInput.Command != "" && !isNoisyBashCommand(toolInput.Command) {
			cmd := toolInput.Command
			if len(cmd) > 200 {
				cmd = cmd[:200]
			}
			exit0 := 0
			_ = appendObservation(cfg, rawObservation{Tool: "Bash", Cmd: cmd, Exit: &exit0})
		}
		return nil
	}

	// Skip read-only / noisy tools
	if isNoisyTool(toolName) {
		return nil
	}

	// File-editing tools
	switch toolName {
	case "Edit", "Write", "NotebookEdit":
		var inp struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		}
		_ = json.Unmarshal(input.ToolInput, &inp)
		filePath := inp.FilePath
		if filePath == "" {
			filePath = inp.NotebookPath
		}
		if filePath == "" {
			return nil
		}
		_ = appendObservation(cfg, rawObservation{Tool: toolName, File: filePath})
	}

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
