package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/m4vic/one-context/internal/state"
)

func TestRunPublishesHeartbeatAndStops(t *testing.T) {
	t.Setenv("ONE_CONTEXT_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if status, err := ReadStatus(); err == nil && status.PID == os.Getpid() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not publish a heartbeat")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := RequestStop(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not stop")
	}
	registryPath, _ := state.Path()
	if _, err := os.Stat(filepath.Join(filepath.Dir(registryPath), "daemon.json")); !os.IsNotExist(err) {
		t.Fatalf("status file remained after stop: %v", err)
	}
}

func TestWatcherRetryDelayIsBounded(t *testing.T) {
	if got := watcherRetryDelay(1); got != 5*time.Second {
		t.Fatalf("first retry = %s", got)
	}
	if got := watcherRetryDelay(3); got != 20*time.Second {
		t.Fatalf("third retry = %s", got)
	}
	if got := watcherRetryDelay(20); got != 5*time.Minute {
		t.Fatalf("bounded retry = %s", got)
	}
}
