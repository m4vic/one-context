//go:build windows

package startup

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const windowsTaskName = "one-context"

var runCommand = func(name string, args ...string) error {
	return execCommand(name, args...)
}

var execCommand = func(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func Path() (string, error) {
	return "Task Scheduler: " + windowsTaskName, nil
}

func Install(executable string) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	command := fmt.Sprintf("\"%s\" daemon run", strings.ReplaceAll(executable, "\"", "\"\""))
	if err := runCommand("schtasks", "/Create", "/TN", windowsTaskName, "/TR", command, "/SC", "ONLOGON", "/RL", "LIMITED", "/F"); err != nil {
		return "", fmt.Errorf("create Windows login task: %w", err)
	}
	return path, nil
}

func Remove() error {
	if _, err := Path(); err != nil {
		return err
	}
	err := runCommand("schtasks", "/Delete", "/TN", windowsTaskName, "/F")
	if err != nil {
		return fmt.Errorf("remove Windows login task: %w", err)
	}
	return nil
}

func Installed() (bool, string, error) {
	path, err := Path()
	if err != nil {
		return false, "", err
	}
	err = runCommand("schtasks", "/Query", "/TN", windowsTaskName)
	if err == nil {
		return true, path, nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "cannot find") {
		return false, path, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return false, path, err
	}
	return false, path, nil
}
