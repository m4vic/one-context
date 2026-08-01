package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterAndResolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ONE_CONTEXT_HOME", home)
	repo := t.TempDir()
	project, err := Register("demo", repo)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(reg, repo)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name != project.Name {
		t.Fatalf("resolved %q, want %q", resolved.Name, project.Name)
	}
	if _, err := os.Stat(filepath.Join(home, "registry.json")); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateLLM(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	config := LLMConfig{Provider: "ollama", Model: "qwen3:4b"}
	if err := UpdateLLM(config); err != nil {
		t.Fatal(err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if registry.LLM != config {
		t.Fatalf("LLM config = %#v, want %#v", registry.LLM, config)
	}
}

func TestSetErrorRetainsSuccessfulScanState(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	project, err := Register("demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateScan(project.Name, project.CreatedAt, "fingerprint", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetError(project.Name, "watcher permission denied"); err != nil {
		t.Fatal(err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	updated := registry.Projects[project.Name]
	if updated.LastError != "watcher permission denied" || updated.LastFingerprint != "fingerprint" {
		t.Fatalf("unexpected project state: %#v", updated)
	}
}

func TestReserveLLMRequestHonorsDailyLimit(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	if err := UpdateLLM(LLMConfig{Provider: "api", DailyRequestLimit: 1}); err != nil {
		t.Fatal(err)
	}
	if err := ReserveLLMRequest(); err != nil {
		t.Fatal(err)
	}
	if err := ReserveLLMRequest(); err == nil {
		t.Fatal("expected daily request limit error")
	}
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if registry.LLMUsage.Requests != 1 {
		t.Fatalf("requests = %d", registry.LLMUsage.Requests)
	}
}

func TestLoadMigratesVersionOneRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ONE_CONTEXT_HOME", home)
	legacy := Registry{SchemaVersion: 1, Projects: map[string]Project{"demo": {Name: "demo", Root: t.TempDir(), Enabled: true}}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "registry.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	project := registry.Projects["demo"]
	if registry.SchemaVersion != SchemaVersion || project.ScanInterval != "5s" || project.ScanWindow != "3h" || !project.Enabled {
		t.Fatalf("migration result = %#v", registry)
	}
	if err := Save(registry); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(filepath.Join(home, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), `"schema_version": 2`) {
		t.Fatalf("migration did not persist v2: %s", persisted)
	}
}

func TestRegisterRetainsProjectLLMPolicy(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	root := t.TempDir()
	project, err := Register("demo", root)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	project.LLMAllowed = &disabled
	if err := Update(project); err != nil {
		t.Fatal(err)
	}
	registered, err := Register("demo", root)
	if err != nil {
		t.Fatal(err)
	}
	if registered.LLMAllowed == nil || *registered.LLMAllowed {
		t.Fatalf("LLM policy was lost: %#v", registered.LLMAllowed)
	}
}
