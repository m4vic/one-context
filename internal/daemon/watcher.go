package daemon

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/m4vic/one-context/internal/state"
)

type projectWatcher struct {
	project string
	root    string
	watcher *fsnotify.Watcher
	done    chan struct{}
}

type watcherError struct {
	project string
	err     error
}

func startProjectWatcher(project state.Project, changed chan<- string, failures chan<- watcherError) (*projectWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := addDirectories(watcher, project.Root); err != nil {
		watcher.Close()
		return nil, err
	}
	pw := &projectWatcher{project: project.Name, root: project.Root, watcher: watcher, done: make(chan struct{})}
	go pw.run(changed, failures)
	return pw, nil
}

func (pw *projectWatcher) run(changed chan<- string, failures chan<- watcherError) {
	defer close(pw.done)
	for {
		select {
		case event, ok := <-pw.watcher.Events:
			if !ok {
				return
			}
			if ignoredPath(event.Name) {
				continue
			}
			if event.Has(fsnotify.Create) {
				// fsnotify does not recurse into directories created after startup.
				_ = addDirectories(pw.watcher, event.Name)
			}
			select {
			case changed <- pw.project:
			default:
			}
		case err, ok := <-pw.watcher.Errors:
			if !ok {
				return
			}
			select {
			case failures <- watcherError{project: pw.project, err: fmt.Errorf("filesystem watcher: %w", err)}:
			default:
			}
		}
	}
}

func (pw *projectWatcher) close() {
	_ = pw.watcher.Close()
	<-pw.done
}

func addDirectories(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		if path != root && ignoredPath(path) {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

func ignoredPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		switch strings.ToLower(part) {
		case ".git", ".one-context", "node_modules", ".venv":
			return true
		}
	}
	return false
}
