package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/m4vic/one-context/internal/state"
)

func TestParseStatusRanksRecentDirtyFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := "## feature...origin/feature\x00 M new.go\x00 D deleted.go\x00"
	branch, changes := parseStatus(raw, root, time.Now().Add(-time.Hour))
	if branch != "feature" {
		t.Fatalf("branch = %q", branch)
	}
	if len(changes) != 2 || changes[0].Path != "new.go" || changes[1].Path != "deleted.go" {
		t.Fatalf("unexpected changes: %#v", changes)
	}
}

func TestParseStatusExcludesOldTrackedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "old.go")
	if err := os.WriteFile(path, []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	_, changes := parseStatus("## main\x00 M old.go\x00", root, time.Now().Add(-time.Hour))
	if len(changes) != 0 {
		t.Fatalf("expected no recent changes, got %#v", changes)
	}
}

func TestParseStatusExcludesCompilerOutput(t *testing.T) {
	root := t.TempDir()
	_, changes := parseStatus("## main\x00?? .one-context/\x00?? app.go\x00", root, time.Now().Add(-time.Hour))
	if len(changes) != 1 || changes[0].Path != "app.go" {
		t.Fatalf("unexpected changes: %#v", changes)
	}
}

func TestParseStatusExcludesSensitiveFiles(t *testing.T) {
	root := t.TempDir()
	_, changes := parseStatus("## main\x00 M .env\x00 M deploy/private.key\x00 M app.go\x00", root, time.Now().Add(-time.Hour))
	if len(changes) != 1 || changes[0].Path != "app.go" {
		t.Fatalf("sensitive files entered the working set: %#v", changes)
	}
}

func TestReadAnchorsIsBounded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(string(make([]byte, 5000))), 0o644); err != nil {
		t.Fatal(err)
	}
	anchors := readAnchors(root)
	if len(anchors) != 1 || len(anchors[0].Excerpt) > 2600 {
		t.Fatalf("unexpected anchors: %#v", anchors)
	}
}

func TestWriteSeparatesDeterministicAndLLMContext(t *testing.T) {
	root := t.TempDir()
	snapshot := Snapshot{
		Project: "demo", Root: root, Summary: "deterministic evidence", LLMSummary: "## Current state\nmodel handoff", Compression: "ollama:test",
	}
	if err := Write(snapshot); err != nil {
		t.Fatal(err)
	}
	context, err := os.ReadFile(filepath.Join(root, ".one-context", "context.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(context), "deterministic evidence") || strings.Contains(string(context), "model handoff") {
		t.Fatalf("deterministic context was changed: %s", context)
	}
	llm, err := os.ReadFile(filepath.Join(root, ".one-context", "llm-context.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(llm), "model handoff") {
		t.Fatalf("LLM context missing summary: %s", llm)
	}
}

func TestWriteReplacesStaleLLMContextWithAvailabilityNotice(t *testing.T) {
	root := t.TempDir()
	if err := Write(Snapshot{Project: "demo", Root: root, Summary: "evidence", LLMSummary: "old model summary", Compression: "ollama:test"}); err != nil {
		t.Fatal(err)
	}
	if err := Write(Snapshot{Project: "demo", Root: root, Summary: "new evidence", Compression: "deterministic"}); err != nil {
		t.Fatal(err)
	}
	llm, err := os.ReadFile(filepath.Join(root, ".one-context", "llm-context.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(llm), "old model summary") || !strings.Contains(string(llm), "No LLM summary is currently available") {
		t.Fatalf("LLM context was stale: %s", llm)
	}
}

func TestLatestChangeSurvivesCleanWorkingTree(t *testing.T) {
	change := &state.Change{FromHead: "123456789abc", ToHead: "abcdef123456", Summary: "abc1234 add durable handoff", DiffStat: " context.md | 2 ++", ObservedAt: time.Now().UTC()}
	snapshot := Snapshot{Project: "demo", Root: t.TempDir(), Summary: "evidence", LatestChange: change}
	context := renderMarkdown(snapshot)
	if !strings.Contains(context, "add durable handoff") || !strings.Contains(context, "Latest meaningful change") {
		t.Fatalf("latest change missing: %s", context)
	}
}

func TestBuildCapturesCommittedTransition(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "One Context Test")
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "notes.txt")
	runGit(t, root, "commit", "-m", "initial notes")
	first := gitHead(root)
	project := state.Project{Name: "demo", Root: root, LastHead: first}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "notes.txt")
	runGit(t, root, "commit", "-m", "add second note")
	snapshot, err := Build(project, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LatestChange == nil || !strings.Contains(snapshot.LatestChange.Summary, "add second note") {
		t.Fatalf("transition was not captured: %#v", snapshot.LatestChange)
	}
	clean, err := Build(state.Project{Name: "demo", Root: root, LastHead: snapshot.Head, LatestChange: snapshot.LatestChange}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if clean.LatestChange == nil || clean.LatestChange.Summary != snapshot.LatestChange.Summary {
		t.Fatalf("transition was not retained on clean tree: %#v", clean.LatestChange)
	}
}

func TestBuildDoesNotExposeCredentialDiffs(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "One Context Test")
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial files")
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=secret-value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package app\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Build(state.Project{Name: "demo", Root: root}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot.DiffExcerpt, "secret-value") || strings.Contains(snapshot.DiffStat, ".env") {
		t.Fatalf("credential diff leaked into context: %#v", snapshot)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
