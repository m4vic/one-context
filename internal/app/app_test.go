package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m4vic/one-context/internal/compiler"
	"github.com/m4vic/one-context/internal/state"
)

func TestNormalizeOptionAfterProject(t *testing.T) {
	got, err := normalizeOption([]string{"demo", "--since", "2h"}, "--since")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--since", "2h", "demo"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalized args = %#v, want %#v", got, want)
		}
	}
}

func TestNormalizeOptionRequiresValue(t *testing.T) {
	if _, err := normalizeOption([]string{"demo", "--title"}, "--title"); err == nil {
		t.Fatal("expected missing value error")
	}
}

func TestValidateChecksGeneratedArtifactsWithoutRefresh(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	root := t.TempDir()
	project, err := state.Register("demo", root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".one-context")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "context.md"), []byte("context"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(compiler.Snapshot{SchemaVersion: state.SchemaVersion, Project: project.Name, Root: project.Root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := Run([]string{"validate", "demo"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "OK   demo: artifacts valid\n" {
		t.Fatalf("validate output = %q", got)
	}
}

func TestClaudeInstallRequiresRegisteredProject(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if err := Run([]string{"install", "claude", t.TempDir()}, &out, &errOut); err == nil {
		t.Fatal("expected unregistered project error")
	}
}

func TestPaintRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := paint("31", "failure"); got != "failure" {
		t.Fatalf("paint = %q", got)
	}
}

func TestStatusJSON(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	if _, err := state.Register("demo", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := Run([]string{"status", "--json", "demo"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name": "demo"`) {
		t.Fatalf("unexpected JSON: %s", out.String())
	}
}

func TestVersionCommand(t *testing.T) {
	previous := Version
	Version = "v0.1.0-alpha.1"
	t.Cleanup(func() { Version = previous })
	var out, errOut bytes.Buffer
	if err := Run([]string{"version"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != "v0.1.0-alpha.1\n" {
		t.Fatalf("version output = %q", out.String())
	}
}

func TestLLMStatusShowsBudget(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	if err := state.UpdateLLM(state.LLMConfig{Provider: "ollama", Model: "qwen3:4b", DailyRequestLimit: 5}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := Run([]string{"llm"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "0 used, limit 5") {
		t.Fatalf("LLM status = %q", out.String())
	}
}

func TestProjectLLMModeDefaultsToOnAndCanBeDisabled(t *testing.T) {
	if projectLLMMode(state.Project{}) != "on" {
		t.Fatal("default project LLM mode should be on")
	}
	disabled := false
	project := state.Project{LLMAllowed: &disabled}
	if projectAllowsLLM(project) || projectLLMMode(project) != "off" {
		t.Fatal("disabled project LLM mode was not honored")
	}
}
