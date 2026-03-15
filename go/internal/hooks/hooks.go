package hooks

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
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
		return userPrompt(store, cfg)
	case "post-tool-use":
		return postToolUse(store, cfg)
	case "exit-plan-mode":
		return exitPlanMode()
	default:
		return fmt.Errorf("unknown hook: %s", hookName)
	}
}

// ensureDashboard spawns the dashboard subprocess if the port is not already bound.
func ensureDashboard(cfg storage.Config) {
	port := os.Getenv("CONTEXT_KEEPER_PORT")
	if port == "" {
		port = "7373"
	}
	// If already listening, nothing to do
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 200e6); err == nil {
		conn.Close()
		return
	}
	// Spawn dashboard subcommand detached from this process
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "dashboard")
	cmd.Env = append(os.Environ(), "CONTEXT_KEEPER_ROOT="+cfg.ProjectRoot)
	cmd.Stdout = nil
	cmd.Stderr = nil
	detachCmd(cmd)
	_ = cmd.Start()
	// Don't wait — fire and forget
}

// ── session-start ─────────────────────────────────────────────────────────────

func sessionStart(store *storage.Storage, cfg storage.Config) error {
	// Auto-start dashboard if not already running
	ensureDashboard(cfg)

	md := markdown.GenerateContextMd(store.DB())
	wsMd := markdown.GenerateWorkspaceContextMd(store.WorkspaceDB(), cfg.ProjectRoot)

	// Write session start timestamp for stop hook
	ctxDir := filepath.Join(cfg.ProjectRoot, ".context")
	_ = os.MkdirAll(ctxDir, 0755)
	_ = os.WriteFile(filepath.Join(ctxDir, "session-start.tmp"), []byte(time.Now().UTC().Format("2006-01-02 15:04:05")), 0644)

	var sb strings.Builder
	sb.WriteString("# context-keeper\n\n")
	sb.WriteString("## MANDATORY RULES — follow without being asked\n\n")
	sb.WriteString("1. **Call `remember()` immediately** after every: decision made, bug fixed, pattern discovered, approach rejected, API endpoint touched, DB schema change, or file→feature mapping learned. Do NOT wait for the user to ask.\n")
	sb.WriteString("2. **Call `search()` before starting any task** — check if relevant memory exists first.\n")
	sb.WriteString("3. **One entry per insight** — never bundle multiple discoveries into one entry.\n\n")
	sb.WriteString("Type → action mapping (use exactly these types):\n")
	sb.WriteString("- made an architectural choice → `decision`\n")
	sb.WriteString("- fixed a bug or hit an error → `gotcha`\n")
	sb.WriteString("- found a reusable solution → `pattern`\n")
	sb.WriteString("- tried something that failed → `rejected`\n")
	sb.WriteString("- project coding convention → `convention`\n")
	sb.WriteString("- which file owns feature X → `file-map`\n")
	sb.WriteString("- API endpoint details → `api-catalog`\n")
	sb.WriteString("- DB table/field/schema → `schema`\n")
	sb.WriteString("- cross-project knowledge → `workspace`\n\n")

	if strings.HasPrefix(md, "<!-- empty -->") {
		sb.WriteString("## Project Memory\n\nNo entries yet — this session is your chance to start building it.\n")
	} else {
		sb.WriteString("## Project Memory (compact — use `get([id])` for full details)\n\n")
		sb.WriteString(md + "\n")
	}

	if wsMd != "" {
		sb.WriteString("\n## Workspace Memory (shared across projects)\n\n")
		sb.WriteString(wsMd + "\n")
	}

	content := sb.String()

	// Print to stdout for Claude Code (which injects hook stdout into system prompt).
	fmt.Print(content)

	// Also write to .github/instructions/ for Copilot CLI, which ignores hook stdout
	// but reads custom instruction files before each prompt.
	writeInstructionsFile(cfg.ProjectRoot, content)

	return nil
}

// writeInstructionsFile writes context-keeper rules and memory to
// .github/instructions/context-keeper.instructions.md so Copilot CLI
// picks it up as custom instructions (hook stdout is ignored by Copilot).
func writeInstructionsFile(projectRoot, content string) {
	dir := filepath.Join(projectRoot, ".github", "instructions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	frontmatter := "---\napplyTo: \"**\"\n---\n\n"
	_ = os.WriteFile(
		filepath.Join(dir, "context-keeper.instructions.md"),
		[]byte(frontmatter+content),
		0644,
	)
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

	// Count prompt hits from tmp file
	promptHits := 0
	hitsFile := filepath.Join(cfg.ProjectRoot, ".context", "prompt-hits.tmp")
	if hitsData, err := os.ReadFile(hitsFile); err == nil {
		for _, line := range strings.Split(string(hitsData), "\n") {
			if strings.TrimSpace(line) != "" {
				promptHits++
			}
		}
		_ = os.Remove(hitsFile)
	}

	gotchaCount := 0
	for _, e := range sessionEntries {
		if e.Type == "gotcha" {
			gotchaCount++
		}
	}
	obsContent := buildSessionSummaryFromObs(obs, gotchaCount, promptHits)

	if err := store.AddSessionSummary(sessionEntries, obsContent); err != nil {
		return err
	}
	cleanInstructionsFile(cfg.ProjectRoot)
	return nil
}

// cleanInstructionsFile removes context-keeper instruction files written during the session.
func cleanInstructionsFile(projectRoot string) {
	dir := filepath.Join(projectRoot, ".github", "instructions")
	_ = os.Remove(filepath.Join(dir, "context-keeper.instructions.md"))
	_ = os.Remove(filepath.Join(dir, "context-keeper-prompt.instructions.md"))
	_ = os.Remove(filepath.Join(dir, "context-keeper-debug.instructions.md"))
	// Remove the instructions dir only if it's now empty
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

// ── user-prompt ───────────────────────────────────────────────────────────────

func userPrompt(store *storage.Storage, cfg storage.Config) error {
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
		sb.WriteString("[context-keeper] Relevant memory found — read before proceeding, then call remember() with anything new you discover:\n")
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
		_ = appendPromptHit(cfg)

		// Also write to a per-prompt instructions file for Copilot CLI.
		// Copilot re-reads instruction files on each new prompt, so writing here
		// means the next user prompt will have this context injected.
		// (one turn late — unavoidable since Copilot reads before hook fires for current prompt)
		writePromptInstructionsFile(cfg.ProjectRoot, sb.String())
	}
	return nil
}

// writePromptInstructionsFile writes per-prompt relevant memory to a separate
// instructions file so Copilot CLI picks it up on the next prompt.
// Uses a separate file from context-keeper.instructions.md to avoid clobbering
// session-start rules and project memory.
func writePromptInstructionsFile(projectRoot, content string) {
	dir := filepath.Join(projectRoot, ".github", "instructions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	frontmatter := "---\napplyTo: \"**\"\n---\n\n"
	_ = os.WriteFile(
		filepath.Join(dir, "context-keeper-prompt.instructions.md"),
		[]byte(frontmatter+content),
		0644,
	)
}

// ── post-tool-use ─────────────────────────────────────────────────────────────

// toolUseInput is the Claude Code hook input format.
type toolUseInput struct {
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
}

// copilotToolUseInput is the Copilot CLI hook input format.
type copilotToolUseInput struct {
	ToolName   string `json:"toolName"`
	ToolArgs   string `json:"toolArgs"` // JSON-encoded string
	ToolResult struct {
		ResultType      string `json:"resultType"`      // "success" | "failure" | "denied"
		TextResultForLlm string `json:"textResultForLlm"`
	} `json:"toolResult"`
}

type rawObservation struct {
	Tool string `json:"tool"`
	File string `json:"file,omitempty"`
	Cmd  string `json:"cmd,omitempty"`
	Exit *int   `json:"exit,omitempty"`
	Kind string `json:"kind,omitempty"` // "explore" | "edit" | "run"
	Ts   string `json:"ts"`
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

func buildSessionSummaryFromObs(obs []rawObservation, gotchaCount int, promptHits int) string {
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

	// Exploration telemetry
	exploreCount, totalCount := 0, len(obs)
	stepsBeforeFirstEdit := -1
	var exploredFiles []string
	exploredSeen := map[string]bool{}

	for i, o := range obs {
		if o.Kind == "explore" {
			exploreCount++
			if o.File != "" {
				base := filepath.Base(o.File)
				if !exploredSeen[base] {
					exploredSeen[base] = true
					exploredFiles = append(exploredFiles, base)
				}
			}
		}
		if stepsBeforeFirstEdit == -1 && o.Kind == "edit" {
			stepsBeforeFirstEdit = i
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
	if totalCount > 0 {
		ratio := float64(exploreCount) / float64(totalCount) * 100
		fmt.Fprintf(&sb, "Exploration ratio: %.0f%% (%d/%d)\n", ratio, exploreCount, totalCount)
		if stepsBeforeFirstEdit >= 0 {
			fmt.Fprintf(&sb, "Steps before first edit: %d\n", stepsBeforeFirstEdit)
		}
		if len(exploredFiles) > 0 {
			shown := exploredFiles
			if len(shown) > 8 {
				shown = shown[:8]
			}
			fmt.Fprintf(&sb, "Files explored (%d): %s\n", len(exploredFiles), strings.Join(shown, ", "))
		}
	}
	if promptHits > 0 {
		fmt.Fprintf(&sb, "Prompt hits: %d (memory injected %d times this session)\n", promptHits, promptHits)
	}
	return sb.String()
}

func postToolUse(store *storage.Storage, cfg storage.Config) error {
	stdinData, err := readStdin()
	if err != nil || len(stdinData) == 0 {
		return nil
	}

	// Try to detect format: Copilot CLI uses camelCase "toolName", Claude Code uses "tool_name".
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(stdinData, &rawMap); err != nil {
		return nil
	}

	if _, isCopilot := rawMap["toolName"]; isCopilot {
		return postToolUseCopilot(store, cfg, stdinData)
	}
	return postToolUseClaudeCode(store, cfg, stdinData)
}

// postToolUseCopilot handles Copilot CLI's postToolUse input format.
func postToolUseCopilot(store *storage.Storage, cfg storage.Config, data []byte) error {
	var input copilotToolUseInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil
	}

	toolName := strings.ToLower(input.ToolName)

	// Parse toolArgs (it's a JSON-encoded string in Copilot CLI)
	var toolArgs map[string]interface{}
	_ = json.Unmarshal([]byte(input.ToolArgs), &toolArgs)

	// Bash / shell failure → auto-capture gotcha
	if toolName == "bash" || toolName == "shell" {
		if input.ToolResult.ResultType == "failure" {
			errorText := input.ToolResult.TextResultForLlm
			cmd := ""
			if c, ok := toolArgs["command"].(string); ok {
				cmd = c
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
		// Success: record observation if not noisy
		if cmd, ok := toolArgs["command"].(string); ok && cmd != "" && !isNoisyBashCommand(cmd) {
			if len(cmd) > 200 {
				cmd = cmd[:200]
			}
			exit0 := 0
			_ = appendObservation(cfg, rawObservation{Tool: "Bash", Cmd: cmd, Exit: &exit0, Kind: "run"})
		}
		return nil
	}

	// Map Copilot tool names to observation kinds.
	switch toolName {
	case "view", "glob", "grep":
		target := ""
		for _, k := range []string{"path", "pattern", "file_path"} {
			if v, ok := toolArgs[k].(string); ok && v != "" {
				target = v
				break
			}
		}
		_ = appendObservation(cfg, rawObservation{Tool: strings.Title(toolName), File: target, Kind: "explore"})
	case "edit", "create":
		filePath := ""
		for _, k := range []string{"path", "file_path"} {
			if v, ok := toolArgs[k].(string); ok && v != "" {
				filePath = v
				break
			}
		}
		if filePath != "" {
			toolLabel := "Edit"
			if toolName == "create" {
				toolLabel = "Write"
			}
			_ = appendObservation(cfg, rawObservation{Tool: toolLabel, File: filePath, Kind: "edit"})
		}
	}
	return nil
}

// postToolUseClaudeCode handles Claude Code's postToolUse input format.
func postToolUseClaudeCode(store *storage.Storage, cfg storage.Config, data []byte) error {
	var input toolUseInput
	if err := json.Unmarshal(data, &input); err != nil {
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
			_ = appendObservation(cfg, rawObservation{Tool: "Bash", Cmd: cmd, Exit: &exit0, Kind: "run"})
		}
		return nil
	}

	// Capture explore observations for read-only tools
	switch toolName {
	case "Read", "Glob", "Grep", "LS":
		var inp struct {
			FilePath string `json:"file_path"`
			Pattern  string `json:"pattern"`
			Path     string `json:"path"`
		}
		_ = json.Unmarshal(input.ToolInput, &inp)
		target := inp.FilePath
		if target == "" {
			target = inp.Pattern
		}
		if target == "" {
			target = inp.Path
		}
		_ = appendObservation(cfg, rawObservation{Tool: toolName, File: target, Kind: "explore"})
		return nil
	case "WebFetch", "WebSearch", "Agent", "TodoRead", "TodoWrite":
		return nil // skip — not project exploration
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
		_ = appendObservation(cfg, rawObservation{Tool: toolName, File: filePath, Kind: "edit"})
	}

	return nil
}

// appendPromptHit increments the per-session prompt hit counter.
func appendPromptHit(cfg storage.Config) error {
	path := filepath.Join(cfg.ProjectRoot, ".context", "prompt-hits.tmp")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, "1")
	return err
}

// ── exit-plan-mode ────────────────────────────────────────────────────────────

func exitPlanMode() error {
	fmt.Print(`[context-keeper] Plan mode exited — you MUST call remember() now:
- Plan REJECTED → remember(type="rejected", title="[Plan] ...", content="what was proposed + why rejected + what you chose instead")
- Plan ACCEPTED → remember(type="decision", title="[Plan] ...", content="what was decided + why") then implement
Do not skip this step.
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

	buildKeywords := []string{"implement", "add", "create", "build", "integrate", "write", "update", "change", "modify", "redesign", "style", "layout", "component"}
	for _, kw := range buildKeywords {
		if strings.Contains(lower, kw) {
			return "if this becomes reusable, save as type 'pattern' or 'decision'"
		}
	}

	refactorKeywords := []string{"refactor", "restructure", "rename", "migrate", "move", "reorganize"}
	for _, kw := range refactorKeywords {
		if strings.Contains(lower, kw) {
			return "architectural change — save as type 'decision'"
		}
	}

	apiKeywords := []string{"endpoint", "route", "handler", "api"}
	for _, kw := range apiKeywords {
		if strings.Contains(lower, kw) {
			return "API-related — document endpoints as type 'api-catalog'"
		}
	}

	schemaKeywords := []string{"table", "schema", "column", "migration"}
	for _, kw := range schemaKeywords {
		if strings.Contains(lower, kw) {
			return "DB-related — document tables/fields as type 'schema'"
		}
	}

	return ""
}
