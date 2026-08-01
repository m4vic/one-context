package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/m4vic/one-context/internal/compiler"
	"github.com/m4vic/one-context/internal/compression"
	"github.com/m4vic/one-context/internal/daemon"
	"github.com/m4vic/one-context/internal/integration"
	"github.com/m4vic/one-context/internal/startup"
	"github.com/m4vic/one-context/internal/state"
)

// Version is replaced by GoReleaser for tagged release artifacts.
var Version = "dev"

func Run(args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return home(out)
	}
	args[0] = strings.TrimPrefix(args[0], "/")
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage(out)
		return nil
	}
	switch args[0] {
	case "add":
		return addProject(args[1:], out, errOut)
	case "pause":
		return setWatching(args[1:], false, out)
	case "resume":
		return setWatching(args[1:], true, out)
	case "install":
		return installIntegration(args[1:], out)
	case "llm":
		return llmCommand(args[1:], out)
	case "init":
		return initProject(args[1:], false, out, errOut)
	case "watch":
		return initProject(args[1:], true, out, errOut)
	case "status":
		return status(args[1:], out)
	case "scan", "sync":
		return scan(args[1:], out, errOut)
	case "task":
		return task(args[1:], out, errOut)
	case "daemon":
		return daemonCommand(args[1:], out)
	case "unwatch":
		return setWatching(args[1:], false, out)
	case "handoff":
		return handoff(args[1:], out, errOut)
	case "doctor":
		return doctor(out)
	case "validate":
		return validate(args[1:], out)
	case "logs":
		return logs(out)
	case "repair":
		return repair(out)
	case "uninstall":
		return uninstall(out)
	case "startup":
		return startupCommand(args[1:], out)
	case "export":
		return exportProject(args[1:], out, errOut)
	case "import":
		return importProject(args[1:], out)
	case "remove":
		return removeProject(args[1:], out)
	case "config":
		return configProject(args[1:], out, errOut)
	case "version":
		fmt.Fprintln(out, Version)
		return nil
	default:
		usage(errOut)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func home(out io.Writer) error {
	banner := []string{
		`     __  _  _  ____     ___ ___  _  _ _____ _____ __  __ _____`,
		`    /__\| \| || __|___/ __/ _ \| \| |_   _| __\ \/ /|_   _|`,
		`   / \_/| .  || _||___| (_| (_) | .  | | | | _| >  <   | |`,
		`   \___/|_|\_||___|    \___\___/|_|\_| |_| |___/_/\_\  |_|`,
	}
	for index, line := range banner {
		fmt.Fprintln(out, paint([]string{"38;5;45", "38;5;81", "38;5;117", "38;5;153"}[index], line))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, paint("2", "Context that follows your work, not your chat window."))
	fmt.Fprintln(out)
	if !interactiveTerminal() {
		return status(nil, out)
	}
	fmt.Fprintln(out, "  /add       Watch a folder and start automatic updates")
	fmt.Fprintln(out, "  /status    Show watched projects")
	fmt.Fprintln(out, "  /integrate Connect Claude Code or Codex")
	fmt.Fprintln(out, "  /llm       Configure local or API compression")
	fmt.Fprintln(out, "  /pause     Pause a project")
	fmt.Fprintln(out, "  /resume    Resume a project")
	fmt.Fprintln(out, "  /repair    Repair background updates")
	fmt.Fprintln(out, "  /logs      Show daemon diagnostics")
	fmt.Fprintln(out, "  /help      Show commands     /quit      Exit")
	fmt.Fprintln(out)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(out, paint("38;5;45", "one-context")+" "+paint("2", ">")+" ")
		if !scanner.Scan() {
			return nil
		}
		if err := runPaletteCommand(strings.TrimSpace(scanner.Text()), scanner, out); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			fmt.Fprintln(out, paint("31", err.Error()))
		}
	}
}

func paint(code, value string) string {
	if os.Getenv("NO_COLOR") != "" || !interactiveTerminal() {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func installIntegration(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: one-context install <claude <folder>|codex|all <folder>>")
	}
	switch strings.ToLower(args[0]) {
	case "claude":
		if len(args) != 2 {
			return errors.New("usage: one-context install claude <folder>")
		}
		project, err := registeredProject(args[1])
		if err != nil {
			return err
		}
		path, err := integration.InstallClaude(project.Root)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Claude Code command installed: %s\nUse /one-context inside Claude Code.\n", path)
		return nil
	case "codex":
		if len(args) != 1 {
			return errors.New("usage: one-context install codex")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path, err := integration.InstallCodex(home)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Codex skill installed: %s\nUse $one-context in a registered project.\n", path)
		return nil
	case "all":
		if len(args) != 2 {
			return errors.New("usage: one-context install all <folder>")
		}
		project, err := registeredProject(args[1])
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		claudePath, codexPath, err := integration.InstallAll(project.Root, home)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Claude Code command installed: %s\nCodex skill installed: %s\nUse /one-context in Claude Code or $one-context in Codex.\n", claudePath, codexPath)
		return nil
	default:
		return fmt.Errorf("unsupported integration %q", args[0])
	}
}

func registeredProject(value string) (state.Project, error) {
	registry, err := state.Load()
	if err != nil {
		return state.Project{}, err
	}
	return state.Resolve(registry, value)
}

func llmCommand(args []string, out io.Writer) error {
	registry, err := state.Load()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if registry.LLM.Provider == "" {
			fmt.Fprintln(out, "LLM compression: off")
			return nil
		}
		limit := "unlimited"
		if registry.LLM.DailyRequestLimit > 0 {
			limit = strconv.Itoa(registry.LLM.DailyRequestLimit)
		}
		fmt.Fprintf(out, "LLM compression: %s (%s)\nDaily requests: %d used, limit %s\n", registry.LLM.Provider, registry.LLM.Model, registry.LLMUsage.Requests, limit)
		return nil
	}
	var config state.LLMConfig
	switch strings.ToLower(args[0]) {
	case "off", "none":
		if err := state.UpdateLLM(config); err != nil {
			return err
		}
		refreshed, err := refreshAllProjects()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "LLM compression disabled. Refreshed %d project(s); context.md remains available.\n", refreshed)
		return nil
	case "limit":
		if len(args) != 2 {
			return errors.New("usage: one-context /llm limit <daily-requests; 0 means unlimited>")
		}
		limit, err := strconv.Atoi(args[1])
		if err != nil || limit < 0 {
			return errors.New("LLM daily request limit must be a non-negative integer")
		}
		config = registry.LLM
		config.DailyRequestLimit = limit
		if err := state.UpdateLLM(config); err != nil {
			return err
		}
		fmt.Fprintf(out, "LLM daily request limit set to %d (0 means unlimited).\n", limit)
		return nil
	case "ollama":
		config.Provider, config.Model = "ollama", "qwen3:4b"
		if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
			config.Model = strings.TrimSpace(args[1])
		}
	case "api", "openai", "anthropic", "claude", "gemini":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("usage: one-context /llm %s <model> [base-url]", args[0])
		}
		config.Provider, config.Model = strings.ToLower(args[0]), strings.TrimSpace(args[1])
		if config.Provider == "openai" {
			config.Provider = "api"
		}
		if config.Provider == "claude" {
			config.Provider = "anthropic"
		}
		if len(args) > 2 {
			config.BaseURL = strings.TrimSpace(args[2])
		}
	default:
		return errors.New("usage: one-context /llm <ollama [model]|api|claude|gemini <model> [base-url]|off>")
	}
	if err := state.UpdateLLM(config); err != nil {
		return err
	}
	refreshed, refreshErr := refreshAllProjects()
	switch config.Provider {
	case "api":
		fmt.Fprintf(out, "OpenAI-compatible compression configured. Refreshed %d project(s). Set ONE_CONTEXT_API_KEY before starting the daemon.\n", refreshed)
	case "anthropic":
		fmt.Fprintf(out, "Claude compression configured. Refreshed %d project(s). Set ANTHROPIC_API_KEY before starting the daemon.\n", refreshed)
	case "gemini":
		fmt.Fprintf(out, "Gemini compression configured. Refreshed %d project(s). Set GEMINI_API_KEY before starting the daemon.\n", refreshed)
	default:
		fmt.Fprintf(out, "Ollama compression configured with model %s. Refreshed %d project(s).\n", config.Model, refreshed)
	}
	return refreshErr
}

// refreshAllProjects makes configuration changes visible immediately instead
// of waiting for an unrelated filesystem event or reconciliation cycle.
func refreshAllProjects() (int, error) {
	registry, err := state.Load()
	if err != nil {
		return 0, err
	}
	refreshed := 0
	for _, name := range state.Names(registry) {
		project := registry.Projects[name]
		if !project.Enabled {
			continue
		}
		if _, err := compileAndRecord(project, parseWindow(project)); err != nil {
			return refreshed, fmt.Errorf("refresh %s: %w", project.Name, err)
		}
		refreshed++
	}
	return refreshed, nil
}

func runPaletteCommand(command string, scanner *bufio.Scanner, out io.Writer) error {
	switch strings.ToLower(command) {
	case "/add", "add":
		folder, err := prompt(scanner, out, "Folder to watch: ")
		if err != nil {
			return err
		}
		return addProject([]string{folder}, out, io.Discard)
	case "/status", "status":
		return status(nil, out)
	case "/integrate", "integrate":
		tool, err := prompt(scanner, out, "Tool [claude/codex/all]: ")
		if err != nil {
			return err
		}
		if strings.EqualFold(tool, "claude") || strings.EqualFold(tool, "all") {
			folder, err := prompt(scanner, out, "Project folder: ")
			if err != nil {
				return err
			}
			return installIntegration([]string{"claude", folder}, out)
		}
		return installIntegration([]string{tool}, out)
	case "/llm", "llm":
		provider, err := prompt(scanner, out, "Provider [ollama/api/claude/gemini/off]: ")
		if err != nil {
			return err
		}
		if strings.EqualFold(provider, "off") {
			return llmCommand([]string{"off"}, out)
		}
		model, err := promptOptional(scanner, out, "Model (blank uses default): ")
		if err != nil {
			return err
		}
		return llmCommand([]string{provider, model}, out)
	case "/repair", "repair":
		return repair(out)
	case "/logs", "logs":
		return logs(out)
	case "/pause", "pause", "/resume", "resume":
		folder, err := prompt(scanner, out, "Project folder: ")
		if err != nil {
			return err
		}
		return setWatching([]string{folder}, strings.Contains(command, "resume"), out)
	case "/help", "help":
		fmt.Fprintln(out, "Use /add, /status, /integrate, /llm, /pause, /resume, or /quit.")
		return nil
	case "/quit", "quit", "exit":
		return io.EOF
	default:
		return errors.New("unknown command; type /help")
	}
}

func prompt(scanner *bufio.Scanner, out io.Writer, label string) (string, error) {
	value, err := promptOptional(scanner, out, label)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", errors.New("a value is required")
	}
	return value, nil
}

func promptOptional(scanner *bufio.Scanner, out io.Writer, label string) (string, error) {
	fmt.Fprint(out, label)
	if !scanner.Scan() {
		return "", errors.New("input cancelled")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func interactiveTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func addProject(args []string, out, errOut io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: one-context add <folder>")
	}
	root, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	name := filepath.Base(filepath.Clean(root))
	reg, err := state.Load()
	if err != nil {
		return err
	}
	project, existingErr := state.Resolve(reg, root)
	isExisting := existingErr == nil
	if !isExisting {
		project, err = state.Register(name, root)
		if err != nil {
			return err
		}
	} else if !project.Enabled {
		project.Enabled = true
		if err := state.Update(project); err != nil {
			return err
		}
	}
	if _, err := compileAndRecord(project, parseWindow(project)); err != nil {
		if !isExisting {
			_ = state.Remove(project.Name)
		}
		return fmt.Errorf("add %s: %w", root, err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	status, err := daemon.Start(executable)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Watching %s\nContext: %s\nDaemon: running (pid %d)\n", project.Root, filepath.Join(project.Root, ".one-context", "context.md"), status.PID)
	return nil
}

func initProject(args []string, watch bool, out, errOut io.Writer) error {
	args, err := normalizeOption(args, "--path")
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(errOut)
	path := fs.String("path", "", "repository path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: one-context init <project> [--path <repo>]")
	}
	registerName := fs.Arg(0)
	if watch && *path == "" {
		reg, err := state.Load()
		if err != nil {
			return err
		}
		project, err := state.Resolve(reg, fs.Arg(0))
		if err != nil {
			if info, statErr := os.Stat(fs.Arg(0)); statErr == nil && info.IsDir() {
				*path = fs.Arg(0)
				registerName = filepath.Base(filepath.Clean(*path))
			} else {
				return errors.New("unregistered project requires --path")
			}
		} else {
			project.Enabled = true
			if err := state.Update(project); err != nil {
				return err
			}
			fmt.Fprintf(out, "Automatic scanning for %s: true\n", project.Name)
			return nil
		}
	}
	if *path == "" {
		*path = "."
	}
	project, err := state.Register(registerName, *path)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Registered %s at %s\n", project.Name, project.Root)
	_, err = compileAndRecord(project, 3*time.Hour)
	if err == nil {
		fmt.Fprintf(out, "Wrote %s\n", filepath.Join(project.Root, ".one-context", "context.md"))
	}
	return err
}

func status(args []string, out io.Writer) error {
	jsonOutput := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		filtered = append(filtered, arg)
	}
	args = filtered
	reg, err := state.Load()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if jsonOutput {
			projects := make([]state.Project, 0, len(reg.Projects))
			for _, name := range state.Names(reg) {
				projects = append(projects, reg.Projects[name])
			}
			return writeJSON(out, projects)
		}
		if len(reg.Projects) == 0 {
			fmt.Fprintln(out, "No projects registered. Run: one-context add <folder>")
			return nil
		}
		for _, name := range state.Names(reg) {
			p := reg.Projects[name]
			fmt.Fprintf(out, "%s\t%s\tlast scan: %s\n", p.Name, p.Root, formatTime(p.LastScanAt))
		}
		return nil
	}
	if len(args) != 1 {
		return errors.New("usage: one-context status [project]")
	}
	p, err := state.Resolve(reg, args[0])
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSON(out, p)
	}
	fmt.Fprintf(out, "Project: %s\nRoot: %s\nWatching: %t\nInterval: %s\nWindow: %s\nLast scan: %s\n", p.Name, p.Root, p.Enabled, p.ScanInterval, p.ScanWindow, formatTime(p.LastScanAt))
	if p.LastError != "" {
		fmt.Fprintf(out, "Last error: %s\n", p.LastError)
	}
	fmt.Fprintf(out, "Artifact: %s\n", filepath.Join(p.Root, ".one-context", "context.md"))
	if p.Task != nil {
		fmt.Fprintf(out, "Task: %s [%s]\n", p.Task.Title, p.Task.Status)
	}
	return nil
}

func writeJSON(out io.Writer, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

func scan(args []string, out, errOut io.Writer) error {
	args, err := normalizeOption(args, "--since")
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(errOut)
	sinceText := fs.String("since", "3h", "recent file window, for example 30m or 3h")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: one-context scan <project> [--since 3h]")
	}
	since, err := time.ParseDuration(*sinceText)
	if err != nil || since <= 0 {
		return fmt.Errorf("invalid --since duration %q", *sinceText)
	}
	reg, err := state.Load()
	if err != nil {
		return err
	}
	p, err := state.Resolve(reg, fs.Arg(0))
	if err != nil {
		return err
	}
	snapshot, err := compileAndRecord(p, since)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Compiled %s: %d working files on branch %s\n", p.Name, len(snapshot.WorkingSet), snapshot.Branch)
	return nil
}

func task(args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: one-context task <set|list|done> ...")
	}
	reg, err := state.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(args) != 2 {
			return errors.New("usage: one-context task list <project>")
		}
		p, err := state.Resolve(reg, args[1])
		if err != nil {
			return err
		}
		if p.Task == nil {
			fmt.Fprintln(out, "No active task.")
		} else {
			fmt.Fprintf(out, "%s [%s]\n", p.Task.Title, p.Task.Status)
		}
		return nil
	case "set":
		normalized, err := normalizeOption(args[1:], "--title")
		if err != nil {
			return err
		}
		fs := flag.NewFlagSet("task set", flag.ContinueOnError)
		fs.SetOutput(errOut)
		title := fs.String("title", "", "task title")
		if err := fs.Parse(normalized); err != nil {
			return err
		}
		if fs.NArg() != 1 || strings.TrimSpace(*title) == "" {
			return errors.New("usage: one-context task set <project> --title <title>")
		}
		p, err := state.Resolve(reg, fs.Arg(0))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		p.Task = &state.Task{Title: strings.TrimSpace(*title), Status: "active", StartedAt: now, UpdatedAt: now}
		if err := state.Update(p); err != nil {
			return err
		}
		if _, err := compileAndRecord(p, parseWindow(p)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Active task set for %s.\n", p.Name)
		return nil
	case "done":
		if len(args) != 2 {
			return errors.New("usage: one-context task done <project>")
		}
		p, err := state.Resolve(reg, args[1])
		if err != nil {
			return err
		}
		if p.Task == nil {
			return errors.New("project has no active task")
		}
		completed := *p.Task
		completed.Status = "done"
		completed.UpdatedAt = time.Now().UTC()
		p.CompletedTasks = append(p.CompletedTasks, completed)
		if len(p.CompletedTasks) > 20 {
			p.CompletedTasks = p.CompletedTasks[len(p.CompletedTasks)-20:]
		}
		p.Task = nil
		if err := state.Update(p); err != nil {
			return err
		}
		if _, err := compileAndRecord(p, parseWindow(p)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Active task cleared for %s.\n", p.Name)
		return nil
	default:
		return fmt.Errorf("unknown task command %q", args[0])
	}
}

func compileAndRecord(project state.Project, since time.Duration) (compiler.Snapshot, error) {
	snapshot, err := compiler.Build(project, since)
	if err != nil {
		return snapshot, err
	}
	registry, err := state.Load()
	if err != nil {
		return snapshot, err
	}
	if projectAllowsLLM(project) {
		if err := enhanceSnapshot(context.Background(), &snapshot, registry.LLM); err != nil {
			snapshot.CompressionError = err.Error()
		}
	}
	if err := compiler.Write(snapshot); err != nil {
		return snapshot, err
	}
	project.LastScanAt = snapshot.GeneratedAt
	project.LastFingerprint = snapshot.Fingerprint
	project.LastError = ""
	project.LastHead = snapshot.Head
	project.LatestChange = snapshot.LatestChange
	return snapshot, state.Update(project)
}

func projectAllowsLLM(project state.Project) bool {
	return project.LLMAllowed == nil || *project.LLMAllowed
}

func enhanceSnapshot(ctx context.Context, snapshot *compiler.Snapshot, config state.LLMConfig) error {
	if compression.Provider(config) == "" {
		return nil
	}
	if err := state.ReserveLLMRequest(); err != nil {
		return err
	}
	return compression.Enhance(ctx, snapshot, config)
}

func daemonCommand(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: one-context daemon <run|start|stop|status>")
	}
	switch args[0] {
	case "run":
		fmt.Fprintln(out, "one-context daemon running; press Ctrl+C to stop")
		return daemon.Run(context.Background())
	case "start":
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		status, err := daemon.Start(executable)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Daemon running (pid %d).\n", status.PID)
		return nil
	case "stop":
		if err := daemon.RequestStop(); err != nil {
			return err
		}
		fmt.Fprintln(out, "Stop requested.")
		return nil
	case "status":
		fmt.Fprintln(out, daemon.Describe())
		return nil
	default:
		return fmt.Errorf("unknown daemon command %q", args[0])
	}
}

func setWatching(args []string, enabled bool, out io.Writer) error {
	if len(args) != 1 {
		if enabled {
			return errors.New("usage: one-context resume <folder>")
		}
		return errors.New("usage: one-context pause <folder>")
	}
	reg, err := state.Load()
	if err != nil {
		return err
	}
	project, err := state.Resolve(reg, args[0])
	if err != nil {
		return err
	}
	project.Enabled = enabled
	if err := state.Update(project); err != nil {
		return err
	}
	if enabled {
		fmt.Fprintf(out, "Watching %s\n", project.Root)
	} else {
		fmt.Fprintf(out, "Paused %s\n", project.Root)
	}
	return nil
}

func handoff(args []string, out, errOut io.Writer) error {
	normalized, err := normalizeOption(args, "--message")
	if err != nil {
		return err
	}
	normalized, err = normalizeOption(normalized, "--tool")
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("handoff", flag.ContinueOnError)
	fs.SetOutput(errOut)
	message := fs.String("message", "", "what the next assistant needs to know")
	tool := fs.String("tool", "user", "source tool name")
	if err := fs.Parse(normalized); err != nil {
		return err
	}
	if fs.NArg() != 1 || strings.TrimSpace(*message) == "" {
		return errors.New("usage: one-context handoff <project> --message <text> [--tool codex]")
	}
	reg, err := state.Load()
	if err != nil {
		return err
	}
	project, err := state.Resolve(reg, fs.Arg(0))
	if err != nil {
		return err
	}
	project.Handoff = &state.Handoff{Message: strings.TrimSpace(*message), Tool: strings.TrimSpace(*tool), UpdatedAt: time.Now().UTC()}
	if err := state.Update(project); err != nil {
		return err
	}
	if _, err := compileAndRecord(project, parseWindow(project)); err != nil {
		return err
	}
	fmt.Fprintf(out, "Handoff recorded and context refreshed for %s.\n", project.Name)
	return nil
}

func doctor(out io.Writer) error {
	failures := 0
	if path, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(out, "FAIL git: executable not found")
		failures++
	} else {
		fmt.Fprintf(out, "OK   git: %s\n", path)
	}
	registryPath, err := state.Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o700); err != nil {
		fmt.Fprintf(out, "FAIL registry: %v\n", err)
		failures++
	} else {
		fmt.Fprintf(out, "OK   registry: %s\n", registryPath)
	}
	reg, err := state.Load()
	if err != nil {
		fmt.Fprintf(out, "FAIL registry data: %v\n", err)
		failures++
	} else {
		fmt.Fprintf(out, "OK   projects: %d registered\n", len(reg.Projects))
		for _, name := range state.Names(reg) {
			p := reg.Projects[name]
			if _, err := os.Stat(p.Root); err != nil {
				fmt.Fprintf(out, "FAIL %s: %v\n", name, err)
				failures++
			}
		}
	}
	fmt.Fprintf(out, "INFO daemon: %s\n", daemon.Describe())
	provider := os.Getenv("ONE_CONTEXT_LLM")
	if provider == "" {
		provider = "off"
	}
	fmt.Fprintf(out, "INFO compression: %s\n", provider)
	if failures > 0 {
		return fmt.Errorf("doctor found %d problem(s)", failures)
	}
	return nil
}

// validate checks generated artifacts and registry references without
// refreshing, repairing, or otherwise mutating user state.
func validate(args []string, out io.Writer) error {
	if len(args) > 1 {
		return errors.New("usage: one-context validate [project]")
	}
	registry, err := state.Load()
	if err != nil {
		return err
	}
	projects := state.Names(registry)
	if len(args) == 1 {
		project, err := state.Resolve(registry, args[0])
		if err != nil {
			return err
		}
		projects = []string{project.Name}
	}
	failures := 0
	for _, name := range projects {
		project := registry.Projects[name]
		if err := validateProject(project); err != nil {
			fmt.Fprintf(out, "FAIL %s: %v\n", project.Name, err)
			failures++
			continue
		}
		fmt.Fprintf(out, "OK   %s: artifacts valid\n", project.Name)
	}
	if failures > 0 {
		return fmt.Errorf("validation found %d problem(s)", failures)
	}
	return nil
}

func validateProject(project state.Project) error {
	info, err := os.Stat(project.Root)
	if err != nil {
		return fmt.Errorf("project path: %w", err)
	}
	if !info.IsDir() {
		return errors.New("project path is not a directory")
	}
	dir := filepath.Join(project.Root, ".one-context")
	contextPath := filepath.Join(dir, "context.md")
	if info, err := os.Stat(contextPath); err != nil || info.IsDir() {
		if err != nil {
			return fmt.Errorf("context.md: %w", err)
		}
		return errors.New("context.md is a directory")
	}
	data, err := os.ReadFile(filepath.Join(dir, "project.json"))
	if err != nil {
		return fmt.Errorf("project.json: %w", err)
	}
	var snapshot compiler.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("project.json is invalid JSON: %w", err)
	}
	if snapshot.SchemaVersion != 1 && snapshot.SchemaVersion != state.SchemaVersion {
		return fmt.Errorf("project.json schema %d is unsupported", snapshot.SchemaVersion)
	}
	if snapshot.Project != project.Name || filepath.Clean(snapshot.Root) != filepath.Clean(project.Root) {
		return errors.New("project.json does not match the registered project")
	}
	return nil
}

func logs(out io.Writer) error {
	path, err := daemon.LogPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(out, "No daemon log yet. Path: %s\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	const maxLogOutput = 16 << 10
	if len(data) > maxLogOutput {
		data = append([]byte("... [earlier log lines omitted]\n"), data[len(data)-maxLogOutput:]...)
	}
	fmt.Fprintf(out, "Daemon log: %s\n%s", path, data)
	return nil
}

func repair(out io.Writer) error {
	registry, err := state.Load()
	if err != nil {
		return err
	}
	refreshed := 0
	for _, name := range state.Names(registry) {
		project := registry.Projects[name]
		if !project.Enabled {
			continue
		}
		if _, err := compileAndRecord(project, parseWindow(project)); err != nil {
			return fmt.Errorf("repair %s: %w", project.Name, err)
		}
		refreshed++
	}
	if _, err := daemon.ReadStatus(); err != nil {
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return executableErr
		}
		if _, startErr := daemon.Start(executable); startErr != nil {
			return startErr
		}
	}
	fmt.Fprintf(out, "Repair complete: refreshed %d enabled project(s); daemon %s.\n", refreshed, daemon.Describe())
	return nil
}

func uninstall(out io.Writer) error {
	if err := startup.Remove(); err != nil {
		return err
	}
	if err := daemon.RequestStop(); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(out, "Login startup removed. Daemon was already stopped or unavailable: %v\n", err)
		return nil
	}
	fmt.Fprintln(out, "Login startup removed and daemon stop requested. Project artifacts and registry entries were kept.")
	return nil
}

func startupCommand(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: one-context startup <install|remove|status>")
	}
	switch args[0] {
	case "install":
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		path, err := startup.Install(executable)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Installed login startup: %s\n", path)
		return nil
	case "remove":
		if err := startup.Remove(); err != nil {
			return err
		}
		fmt.Fprintln(out, "Removed login startup.")
		return nil
	case "status":
		installed, path, err := startup.Installed()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Installed: %t\nPath: %s\n", installed, path)
		return nil
	default:
		return fmt.Errorf("unknown startup command %q", args[0])
	}
}

func exportProject(args []string, out, errOut io.Writer) error {
	normalized, err := normalizeOption(args, "--output")
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(errOut)
	output := fs.String("output", "", "output JSON file")
	if err := fs.Parse(normalized); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: one-context export <project> [--output file.json]")
	}
	reg, err := state.Load()
	if err != nil {
		return err
	}
	project, err := state.Resolve(reg, fs.Arg(0))
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if *output == "" {
		_, err = out.Write(data)
		return err
	}
	if err := state.AtomicWrite(*output, data); err != nil {
		return err
	}
	fmt.Fprintf(out, "Exported %s to %s\n", project.Name, *output)
	return nil
}

func importProject(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: one-context import <file.json>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var imported state.Project
	if err := json.Unmarshal(data, &imported); err != nil {
		return fmt.Errorf("parse import: %w", err)
	}
	if imported.Name == "" || imported.Root == "" {
		return errors.New("import is missing project name or root")
	}
	project, err := state.Register(imported.Name, imported.Root)
	if err != nil {
		return err
	}
	project.Task, project.CompletedTasks, project.Handoff, project.LLMAllowed = imported.Task, imported.CompletedTasks, imported.Handoff, imported.LLMAllowed
	project.ScanInterval, project.ScanWindow, project.Enabled = imported.ScanInterval, imported.ScanWindow, imported.Enabled
	if project.ScanInterval == "" {
		project.ScanInterval = "5s"
	}
	if project.ScanWindow == "" {
		project.ScanWindow = "3h"
	}
	if err := state.Update(project); err != nil {
		return err
	}
	fmt.Fprintf(out, "Imported %s at %s\n", project.Name, project.Root)
	return nil
}

func removeProject(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: one-context remove <project>")
	}
	if err := state.Remove(args[0]); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed %s from the registry; repository files were not deleted.\n", args[0])
	return nil
}

func configProject(args []string, out, errOut io.Writer) error {
	normalized, err := normalizeOption(args, "--interval")
	if err != nil {
		return err
	}
	normalized, err = normalizeOption(normalized, "--window")
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(errOut)
	intervalText := fs.String("interval", "", "automatic scan interval")
	windowText := fs.String("window", "", "recent working-set window")
	llmText := fs.String("llm", "", "allow or deny LLM compression for this project")
	if err := fs.Parse(normalized); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: one-context config <project> [--interval 5s] [--window 3h] [--llm on|off]")
	}
	reg, err := state.Load()
	if err != nil {
		return err
	}
	project, err := state.Resolve(reg, fs.Arg(0))
	if err != nil {
		return err
	}
	if *intervalText != "" {
		interval, err := time.ParseDuration(*intervalText)
		if err != nil || interval < time.Second {
			return errors.New("--interval must be at least 1s")
		}
		project.ScanInterval = interval.String()
	}
	if *windowText != "" {
		window, err := time.ParseDuration(*windowText)
		if err != nil || window <= 0 {
			return errors.New("--window must be a positive duration")
		}
		project.ScanWindow = window.String()
	}
	if *llmText != "" {
		switch strings.ToLower(*llmText) {
		case "on", "allow":
			value := true
			project.LLMAllowed = &value
		case "off", "deny":
			value := false
			project.LLMAllowed = &value
		default:
			return errors.New("--llm must be on or off")
		}
	}
	if err := state.Update(project); err != nil {
		return err
	}
	fmt.Fprintf(out, "Project %s: interval=%s window=%s llm=%s\n", project.Name, project.ScanInterval, project.ScanWindow, projectLLMMode(project))
	return nil
}

func projectLLMMode(project state.Project) string {
	if project.LLMAllowed == nil || *project.LLMAllowed {
		return "on"
	}
	return "off"
}

func parseWindow(project state.Project) time.Duration {
	window, err := time.ParseDuration(project.ScanWindow)
	if err != nil || window <= 0 {
		return 3 * time.Hour
	}
	return window
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "never"
	}
	return value.Local().Format(time.RFC3339)
}

func usage(out io.Writer) {
	fmt.Fprintln(out, `one-context compiles repository state into assistant-ready context.

Usage:
  one-context                         Open the slash-command palette
	  one-context version                 Print binary version
  one-context /add <folder>           Start watching a project
  one-context /status [--json]        Show project health
  one-context /pause <folder>         Pause a project
  one-context /resume <folder>        Resume a project
  one-context /install claude <folder> Add Claude Code /one-context
	  one-context /install codex          Add the Codex $one-context skill
	  one-context /install all <folder>   Add both bridges for a registered project
  one-context /llm ollama [model]     Use a local Ollama model
	  one-context /llm api <model> [url]  Use an OpenAI-compatible API
	  one-context /llm claude <model> [url] Use the Anthropic Messages API
  one-context /llm gemini <model> [url] Use the Gemini GenerateContent API
	  one-context /llm limit <n>          Set daily provider request limit (0 = unlimited)
  one-context /llm off                Disable model compression
  one-context /doctor                 Diagnose local dependencies
	  one-context validate [project]     Check artifacts without changing them
	  one-context /logs                  Show local daemon diagnostics
	  one-context /repair                Refresh enabled projects and restart daemon if needed
	  one-context /uninstall             Remove login startup without deleting project data

Inside the palette:
  /add  /status  /integrate  /llm  /pause  /resume  /help  /quit`)
}

// normalizeOption accepts both conventional flag-first input and the more
// readable command shape used in the documentation: <project> --flag <value>.
func normalizeOption(args []string, option string) ([]string, error) {
	position := -1
	for i, arg := range args {
		if arg == option {
			position = i
			break
		}
	}
	if position == -1 || position == 0 {
		return args, nil
	}
	if position+1 >= len(args) {
		return nil, fmt.Errorf("%s requires a value", option)
	}
	normalized := []string{option, args[position+1]}
	normalized = append(normalized, args[:position]...)
	normalized = append(normalized, args[position+2:]...)
	return normalized, nil
}
