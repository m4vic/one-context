//go:build !windows

package startup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	linuxUnit = "one-context.service"
	macLabel  = "com.one-context.daemon"
	macPlist  = macLabel + ".plist"
)

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		return filepath.Join(config, "systemd", "user", linuxUnit), nil
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", macPlist), nil
	default:
		return "", fmt.Errorf("startup installation is not supported on %s", runtime.GOOS)
	}
}

func Install(executable string) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	var content string
	switch runtime.GOOS {
	case "linux":
		content = "[Unit]\nDescription=one-context background context compiler\n\n[Service]\nType=simple\nExecStart=" + systemdEscape(executable) + " daemon run\nRestart=on-failure\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n"
	case "darwin":
		content = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>daemon</string><string>run</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/></dict></plist>
`, macLabel, xmlEscape(executable))
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := activate(path); err != nil {
		return path, err
	}
	return path, nil
}

func Remove() error {
	path, err := Path()
	if err != nil {
		return err
	}
	_ = deactivate(path)
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func Installed() (bool, string, error) {
	path, err := Path()
	if err != nil {
		return false, "", err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, path, nil
	}
	return err == nil, path, err
}

func activate(path string) error {
	switch runtime.GOOS {
	case "linux":
		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			return err
		}
		return exec.Command("systemctl", "--user", "enable", "--now", linuxUnit).Run()
	case "darwin":
		return exec.Command("launchctl", "bootstrap", "gui/"+fmt.Sprint(os.Getuid()), path).Run()
	default:
		return errors.New("unsupported startup platform")
	}
}

func deactivate(path string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("systemctl", "--user", "disable", "--now", linuxUnit).Run()
	case "darwin":
		return exec.Command("launchctl", "bootout", "gui/"+fmt.Sprint(os.Getuid()), path).Run()
	default:
		return nil
	}
}

func systemdEscape(value string) string {
	return strings.ReplaceAll(value, " ", "\\x20")
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
