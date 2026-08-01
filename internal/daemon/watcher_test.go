package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/m4vic/one-context/internal/state"
)

func TestProjectWatcherReportsFileChanges(t *testing.T) {
	repo := t.TempDir()
	changes := make(chan string, 1)
	failures := make(chan watcherError, 1)
	watcher, err := startProjectWatcher(state.Project{Name: "demo", Root: repo}, changes, failures)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case project := <-changes:
		if project != "demo" {
			t.Fatalf("project = %q, want demo", project)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not report file change")
	}
}

func TestIgnoredPath(t *testing.T) {
	for _, path := range []string{"repo/.git/index", "repo/.one-context/context.md", "repo/node_modules/pkg/index.js"} {
		if !ignoredPath(path) {
			t.Fatalf("expected %q to be ignored", path)
		}
	}
	if ignoredPath("repo/internal/app.go") {
		t.Fatal("source file should not be ignored")
	}
}
