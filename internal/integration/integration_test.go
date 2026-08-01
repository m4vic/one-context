package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallClaudeIsIdempotent(t *testing.T) {
	path, err := InstallClaude(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallClaude(filepath.Dir(filepath.Dir(filepath.Dir(path)))); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != claudeCommand {
		t.Fatal("unexpected Claude command content")
	}
}

func TestInstallCodexRefusesOverwrite(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "skills", "one-context", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallCodex(home); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func TestInstallAllCreatesOnlyAdapterFiles(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	claudePath, codexPath, err := InstallAll(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(claudePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(codexPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".one-context")); !os.IsNotExist(err) {
		t.Fatalf("adapter created a second context store: %v", err)
	}
}
