package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/m4vic/one-context/internal/state"
)

type Commit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
}

type FileChange struct {
	Path       string    `json:"path"`
	Status     string    `json:"status"`
	ModifiedAt time.Time `json:"modified_at,omitempty"`
}

type Anchor struct {
	Path    string `json:"path"`
	Excerpt string `json:"excerpt"`
}

type Snapshot struct {
	SchemaVersion    int            `json:"schema_version"`
	GeneratedAt      time.Time      `json:"generated_at"`
	Project          string         `json:"project"`
	Root             string         `json:"root"`
	Branch           string         `json:"branch"`
	Head             string         `json:"head,omitempty"`
	Task             *state.Task    `json:"active_task,omitempty"`
	RecentCommits    []Commit       `json:"recent_commits"`
	WorkingSet       []FileChange   `json:"working_set"`
	Fingerprint      string         `json:"fingerprint"`
	DiffStat         string         `json:"diff_stat,omitempty"`
	DiffExcerpt      string         `json:"diff_excerpt,omitempty"`
	Summary          string         `json:"summary"`
	Compression      string         `json:"compression"`
	CompressionError string         `json:"compression_error,omitempty"`
	LLMSummary       string         `json:"llm_summary,omitempty"`
	Handoff          *state.Handoff `json:"handoff,omitempty"`
	Anchors          []Anchor       `json:"anchors,omitempty"`
	LatestChange     *state.Change  `json:"latest_change,omitempty"`
}

func Build(project state.Project, since time.Duration) (Snapshot, error) {
	status, err := git(project.Root, "status", "--porcelain=v1", "--branch", "-z")
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect git repository: %w", err)
	}
	branch, changes := parseStatus(status, project.Root, time.Now().Add(-since))
	head := gitHead(project.Root)
	commits := recentCommits(project.Root)
	diffArgs := safeDiffPathspec()
	diffStat, _ := git(project.Root, append([]string{"diff", "HEAD", "--stat", "--no-color", "--"}, diffArgs...)...)
	diffExcerpt, _ := git(project.Root, append([]string{"diff", "HEAD", "--unified=0", "--no-color", "--"}, diffArgs...)...)
	diffStat = truncate(diffStat, 4000)
	diffExcerpt = truncate(diffExcerpt, 12000)
	anchors := readAnchors(project.Root)
	latest := latestChange(project, head)
	snapshot := Snapshot{
		SchemaVersion: state.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Project:       project.Name,
		Root:          project.Root,
		Branch:        branch,
		Head:          head,
		Task:          project.Task,
		RecentCommits: commits,
		WorkingSet:    changes,
		DiffStat:      diffStat,
		DiffExcerpt:   diffExcerpt,
		Compression:   "deterministic",
		Handoff:       project.Handoff,
		Anchors:       anchors,
		LatestChange:  latest,
	}
	snapshot.Summary = deterministicSummary(snapshot)
	RefreshFingerprint(&snapshot)
	return snapshot, nil
}

func Compile(project state.Project, since time.Duration) (Snapshot, error) {
	snapshot, err := Build(project, since)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, Write(snapshot)
}

func RefreshFingerprint(snapshot *Snapshot) {
	payload := struct {
		Branch   string
		Head     string
		Task     *state.Task
		Commits  []Commit
		Files    []FileChange
		DiffStat string
		Diff     string
		Handoff  *state.Handoff
		Anchors  []Anchor
		Latest   *state.Change
	}{snapshot.Branch, snapshot.Head, snapshot.Task, snapshot.RecentCommits, snapshot.WorkingSet, snapshot.DiffStat, snapshot.DiffExcerpt, snapshot.Handoff, snapshot.Anchors, snapshot.LatestChange}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	snapshot.Fingerprint = fmt.Sprintf("%x", sum[:])
}

func deterministicSummary(snapshot Snapshot) string {
	parts := []string{fmt.Sprintf("Branch %s has %d files in the current working set.", snapshot.Branch, len(snapshot.WorkingSet))}
	if snapshot.Task != nil {
		parts = append(parts, "Active task: "+snapshot.Task.Title+".")
	}
	if snapshot.DiffStat != "" {
		parts = append(parts, "Git diff stat:\n"+snapshot.DiffStat)
	}
	if snapshot.LatestChange != nil {
		parts = append(parts, "Latest committed change: "+snapshot.LatestChange.Summary+".")
	}
	return strings.Join(parts, "\n\n")
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n... [truncated by one-context]"
}

func readAnchors(root string) []Anchor {
	candidates := []string{"AGENTS.md", "CLAUDE.md", "README.md", "architecture.md", "implementation.md", "IMPLEMENTATION_PLAN.md", filepath.Join("docs", "architecture.md")}
	anchors := make([]Anchor, 0, len(candidates))
	remaining := 10000
	for _, relative := range candidates {
		if remaining <= 0 {
			break
		}
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			continue
		}
		limit := 2500
		if remaining < limit {
			limit = remaining
		}
		excerpt := truncate(string(data), limit)
		anchors = append(anchors, Anchor{Path: filepath.ToSlash(relative), Excerpt: excerpt})
		remaining -= len(excerpt)
	}
	return anchors
}

func Write(snapshot Snapshot) error {
	dir := filepath.Join(snapshot.Root, ".one-context")
	jsonData, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	jsonData = append(jsonData, '\n')
	if err := state.AtomicWrite(filepath.Join(dir, "project.json"), jsonData); err != nil {
		return err
	}
	if err := state.AtomicWrite(filepath.Join(dir, "context.md"), []byte(renderMarkdown(snapshot))); err != nil {
		return err
	}
	return state.AtomicWrite(filepath.Join(dir, "llm-context.md"), []byte(renderLLMMarkdown(snapshot)))
}

func gitHead(root string) string {
	head, err := git(root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(head)
}

func latestChange(project state.Project, head string) *state.Change {
	if head == "" {
		return project.LatestChange
	}
	if project.LastHead == "" || project.LastHead == head {
		return project.LatestChange
	}
	diffStat, _ := git(project.Root, "diff", "--stat", "--no-color", project.LastHead, head)
	commits, _ := git(project.Root, "log", "--format=%h %s", "--no-merges", project.LastHead+".."+head)
	summary := strings.TrimSpace(truncate(commits, 1200))
	if summary == "" {
		summary = "Git history changed from " + shortHead(project.LastHead) + " to " + shortHead(head)
	}
	return &state.Change{FromHead: project.LastHead, ToHead: head, Summary: summary, DiffStat: truncate(diffStat, 4000), ObservedAt: time.Now().UTC()}
}

func shortHead(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func parseStatus(raw string, root string, recentCutoff time.Time) (string, []FileChange) {
	parts := strings.Split(raw, "\x00")
	branch := "unknown"
	changes := make([]FileChange, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "## ") {
			head := strings.TrimPrefix(entry, "## ")
			fields := strings.Fields(strings.Split(head, "...")[0])
			if len(fields) > 0 {
				branch = fields[0]
			}
			continue
		}
		if len(entry) < 4 {
			continue
		}
		code, path := entry[:2], entry[3:]
		if code[0] == 'R' || code[0] == 'C' {
			i++ // porcelain -z adds the original path as the next record
		}
		if compilerOwned(path) || sensitivePath(path) {
			continue
		}
		change := FileChange{Path: filepath.ToSlash(path), Status: strings.TrimSpace(code)}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err == nil {
			change.ModifiedAt = info.ModTime().UTC()
		}
		if code == "??" || change.ModifiedAt.IsZero() || change.ModifiedAt.After(recentCutoff) {
			changes = append(changes, change)
		}
	}
	sort.SliceStable(changes, func(i, j int) bool {
		return changes[i].ModifiedAt.After(changes[j].ModifiedAt)
	})
	return branch, changes
}

func compilerOwned(path string) bool {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	return clean == ".one-context" || strings.HasPrefix(clean, ".one-context/")
}

func safeDiffPathspec() []string {
	return []string{
		".",
		":(exclude).one-context",
		":(exclude)**/.one-context/**",
		":(exclude).env",
		":(exclude).env.*",
		":(exclude)**/.env",
		":(exclude)**/.env.*",
		":(exclude)**/*.pem",
		":(exclude)**/*.key",
		":(exclude)**/id_rsa",
		":(exclude)**/id_ed25519",
	}
}

func sensitivePath(path string) bool {
	base := strings.ToLower(filepath.Base(filepath.ToSlash(path)))
	return base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || base == "id_rsa" || base == "id_ed25519"
}

func recentCommits(root string) []Commit {
	raw, err := git(root, "log", "-5", "--no-merges", "--format=%h%x00%s%x00")
	if err != nil {
		return []Commit{}
	}
	parts := strings.Split(raw, "\x00")
	commits := make([]Commit, 0, 5)
	for i := 0; i+1 < len(parts); i += 2 {
		if strings.TrimSpace(parts[i]) != "" {
			commits = append(commits, Commit{Hash: strings.TrimSpace(parts[i]), Message: strings.TrimSpace(parts[i+1])})
		}
	}
	return commits
}

func git(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git timed out after 10s")
		}
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func renderMarkdown(s Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- Generated by one-context. Do not hand-edit. -->\n\n# %s context\n\n", s.Project)
	fmt.Fprintf(&b, "Generated: %s\n\nRepository: `%s`\n\nBranch: `%s`\n\n", s.GeneratedAt.Format(time.RFC3339), s.Root, s.Branch)
	b.WriteString("## Current task\n\n")
	if s.Task == nil {
		b.WriteString("No active task has been set.\n\n")
	} else {
		fmt.Fprintf(&b, "%s (%s)\n\n", s.Task.Title, s.Task.Status)
	}
	b.WriteString("## Latest handoff\n\n")
	if s.Handoff == nil {
		b.WriteString("No explicit handoff has been recorded.\n\n")
	} else {
		fmt.Fprintf(&b, "%s\n\nRecorded by `%s` at %s.\n\n", s.Handoff.Message, s.Handoff.Tool, s.Handoff.UpdatedAt.Format(time.RFC3339))
	}
	b.WriteString("## Project anchors\n\n")
	if len(s.Anchors) == 0 {
		b.WriteString("No standard project anchor files were found.\n\n")
	} else {
		b.WriteString("These are bounded source excerpts; read the named file when exact or complete instructions matter.\n\n")
		for _, anchor := range s.Anchors {
			fmt.Fprintf(&b, "### `%s`\n\n%s\n\n", anchor.Path, anchor.Excerpt)
		}
	}
	b.WriteString("## Summary\n\n")
	b.WriteString(s.Summary + "\n\n")
	fmt.Fprintln(&b, "This is the deterministic source of truth. Optional model output is in `llm-context.md`.")
	fmt.Fprintln(&b)
	if s.CompressionError != "" {
		fmt.Fprintf(&b, "Compression warning: %s\n\n", s.CompressionError)
	}
	b.WriteString("## Latest meaningful change\n\n")
	if s.LatestChange == nil {
		b.WriteString("No committed change has been observed since one-context started tracking this project.\n\n")
	} else {
		fmt.Fprintf(&b, "%s\n\nObserved: %s. Git: `%s` -> `%s`.\n\n", s.LatestChange.Summary, s.LatestChange.ObservedAt.Format(time.RFC3339), shortHead(s.LatestChange.FromHead), shortHead(s.LatestChange.ToHead))
		if s.LatestChange.DiffStat != "" {
			fmt.Fprintf(&b, "```text\n%s\n```\n\n", s.LatestChange.DiffStat)
		}
	}
	b.WriteString("## Working set\n\n")
	if len(s.WorkingSet) == 0 {
		b.WriteString("No dirty files changed inside the scan window.\n\n")
	} else {
		for _, file := range s.WorkingSet {
			fmt.Fprintf(&b, "- `%s` [%s]\n", file.Path, file.Status)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Recent commits\n\n")
	if len(s.RecentCommits) == 0 {
		b.WriteString("No commits found.\n")
	} else {
		for _, commit := range s.RecentCommits {
			fmt.Fprintf(&b, "- `%s` %s\n", commit.Hash, commit.Message)
		}
	}
	if s.DiffStat != "" {
		b.WriteString("\n## Diff stat\n\n```text\n" + s.DiffStat + "\n```\n")
	}
	return b.String()
}

func renderLLMMarkdown(s Snapshot) string {
	var b strings.Builder
	fmt.Fprintln(&b, "<!-- Generated by one-context. Do not hand-edit. -->")
	fmt.Fprintf(&b, "\n# %s LLM handoff\n\n", s.Project)
	fmt.Fprintf(&b, "Generated: %s\n\nModel: `%s`\n\n", s.GeneratedAt.Format(time.RFC3339), s.Compression)
	fmt.Fprintln(&b, "This is a derived summary. Verify facts against `context.md` and repository files.")
	fmt.Fprintln(&b)
	if s.LLMSummary == "" {
		fmt.Fprintln(&b, "No LLM summary is currently available. Read `context.md` for the deterministic project context.")
	} else {
		b.WriteString(s.LLMSummary + "\n")
	}
	return b.String()
}
