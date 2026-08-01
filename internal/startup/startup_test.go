//go:build windows

package startup

import (
	"strings"
	"testing"
)

func TestInstallAndRemove(t *testing.T) {
	var commands [][]string
	previous := runCommand
	runCommand = func(name string, args ...string) error {
		commands = append(commands, append([]string{name}, args...))
		return nil
	}
	t.Cleanup(func() { runCommand = previous })

	path, err := Install(`C:\Program Files\one-context.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if path != "Task Scheduler: one-context" || len(commands) != 1 {
		t.Fatalf("unexpected install result: path=%q commands=%#v", path, commands)
	}
	if !strings.Contains(strings.Join(commands[0], " "), `"C:\Program Files\one-context.exe" daemon run`) {
		t.Fatalf("missing daemon command: %#v", commands[0])
	}
	if err := Remove(); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.Contains(strings.Join(commands[1], " "), "/Delete") {
		t.Fatalf("missing delete command: %#v", commands)
	}
}
