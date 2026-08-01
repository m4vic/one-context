package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/m4vic/one-context/internal/compiler"
	"github.com/m4vic/one-context/internal/compression"
	"github.com/m4vic/one-context/internal/state"
)

type Status struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Heartbeat time.Time `json:"heartbeat"`
}

const (
	reconcileInterval = 15 * time.Minute
	refreshDebounce   = 2 * time.Second
)

func Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if path, err := stopPath(); err == nil {
		_ = os.Remove(path)
	}
	if err := claim(); err != nil {
		return err
	}
	defer release()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	scheduler := newScheduler(runCtx, 2)
	defer scheduler.close()
	for {
		if err := writeStatus(); err != nil {
			return err
		}
		scheduler.syncWatchers()
		scheduler.collectChanges()
		scheduler.collectFailures()
		scheduler.scanDue()
		scheduler.scanReady()
		if stopped() {
			cancel()
			scheduler.wait()
			return nil
		}
		select {
		case <-ctx.Done():
			scheduler.wait()
			return nil
		case <-ticker.C:
		}
	}
}

type scheduler struct {
	ctx      context.Context
	mu       sync.Mutex
	inFlight map[string]bool
	sem      chan struct{}
	wg       sync.WaitGroup
	watchers map[string]*projectWatcher
	changes  chan string
	failures chan watcherError
	pending  map[string]time.Time
	retryAt  map[string]time.Time
	attempts map[string]int
}

func newScheduler(ctx context.Context, concurrency int) *scheduler {
	return &scheduler{
		ctx: ctx, inFlight: map[string]bool{}, sem: make(chan struct{}, concurrency),
		watchers: map[string]*projectWatcher{}, changes: make(chan string, 128), failures: make(chan watcherError, 64), pending: map[string]time.Time{}, retryAt: map[string]time.Time{}, attempts: map[string]int{},
	}
}

func (s *scheduler) scanDue() {
	reg, err := state.Load()
	if err != nil {
		return
	}
	now := time.Now()
	for _, name := range state.Names(reg) {
		project := reg.Projects[name]
		if !project.Enabled {
			continue
		}
		if !project.LastScanAt.IsZero() && now.Sub(project.LastScanAt) < reconcileInterval {
			continue
		}
		s.startScan(project)
	}
}

func (s *scheduler) syncWatchers() {
	reg, err := state.Load()
	if err != nil {
		return
	}
	active := make(map[string]state.Project, len(reg.Projects))
	for _, name := range state.Names(reg) {
		project := reg.Projects[name]
		if project.Enabled {
			active[name] = project
		}
	}
	for name, watcher := range s.watchers {
		project, ok := active[name]
		if !ok || project.Root != watcher.root {
			watcher.close()
			delete(s.watchers, name)
			delete(s.retryAt, name)
			delete(s.attempts, name)
		}
	}
	now := time.Now()
	for name, project := range active {
		if _, ok := s.watchers[name]; ok {
			continue
		}
		if retry, ok := s.retryAt[name]; ok && now.Before(retry) {
			continue
		}
		watcher, err := startProjectWatcher(project, s.changes, s.failures)
		if err == nil {
			s.watchers[name] = watcher
			delete(s.retryAt, name)
			delete(s.attempts, name)
			continue
		}
		s.attempts[name]++
		s.retryAt[name] = now.Add(watcherRetryDelay(s.attempts[name]))
		s.recordFailure(project.Name, fmt.Errorf("start filesystem watcher: %w", err))
	}
}

func watcherRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 5 * time.Second
	for i := 1; i < attempt && delay < 5*time.Minute; i++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func (s *scheduler) collectChanges() {
	for {
		select {
		case project := <-s.changes:
			s.pending[project] = time.Now()
		default:
			return
		}
	}
}

func (s *scheduler) collectFailures() {
	for {
		select {
		case failure := <-s.failures:
			s.recordFailure(failure.project, failure.err)
		default:
			return
		}
	}
}

func (s *scheduler) recordFailure(project string, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	_ = state.SetError(project, message)
	logf("project=%s error=%s", project, message)
}

func (s *scheduler) scanReady() {
	reg, err := state.Load()
	if err != nil {
		return
	}
	now := time.Now()
	for name, changedAt := range s.pending {
		if now.Sub(changedAt) < refreshDebounce {
			continue
		}
		project, ok := reg.Projects[name]
		if ok && project.Enabled {
			s.startScan(project)
		}
		delete(s.pending, name)
	}
}

func (s *scheduler) startScan(project state.Project) {
	s.mu.Lock()
	if s.inFlight[project.Name] {
		s.mu.Unlock()
		return
	}
	s.inFlight[project.Name] = true
	s.mu.Unlock()
	s.wg.Add(1)
	go func(project state.Project) {
		defer s.wg.Done()
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		scan(s.ctx, project)
		s.mu.Lock()
		delete(s.inFlight, project.Name)
		s.mu.Unlock()
	}(project)
}

func (s *scheduler) close() {
	for _, watcher := range s.watchers {
		watcher.close()
	}
	s.wait()
}

func (s *scheduler) wait() { s.wg.Wait() }

func scan(ctx context.Context, project state.Project) {
	window, err := time.ParseDuration(project.ScanWindow)
	if err != nil || window <= 0 {
		window = 3 * time.Hour
	}
	snapshot, err := compiler.Build(project, window)
	project.LastScanAt = time.Now().UTC()
	if err != nil {
		project.LastError = err.Error()
		_ = state.RecordScan(project.Name, project.LastScanAt, project.LastFingerprint, project.LastError, project.LastHead, project.LatestChange)
		return
	}
	project.LastError = ""
	registry, registryErr := state.Load()
	if registryErr != nil {
		project.LastError = registryErr.Error()
		_ = state.RecordScan(project.Name, project.LastScanAt, project.LastFingerprint, project.LastError, project.LastHead, project.LatestChange)
		return
	}
	if project.LLMAllowed == nil || *project.LLMAllowed {
		if err := enhanceSnapshot(ctx, &snapshot, registry.LLM); err != nil {
			snapshot.CompressionError = err.Error()
		}
	}
	if snapshot.Fingerprint != project.LastFingerprint {
		if err := compiler.Write(snapshot); err != nil {
			project.LastError = err.Error()
		} else {
			project.LastFingerprint = snapshot.Fingerprint
		}
	}
	if project.LastError == "" {
		project.LastHead = snapshot.Head
		project.LatestChange = snapshot.LatestChange
	}
	_ = state.RecordScan(project.Name, project.LastScanAt, project.LastFingerprint, project.LastError, project.LastHead, project.LatestChange)
}

func enhanceSnapshot(ctx context.Context, snapshot *compiler.Snapshot, config state.LLMConfig) error {
	if compression.Provider(config) == "" {
		return nil
	}
	if err := state.ReserveLLMRequest(); err != nil {
		return err
	}
	return compression.Enhance(ctx, snapshot, config)
}

func ReadStatus() (Status, error) {
	path, err := statusPath()
	if err != nil {
		return Status{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, err
	}
	if time.Since(status.Heartbeat) > 4*time.Second {
		return status, errors.New("daemon heartbeat is stale")
	}
	return status, nil
}

func PID() (int, error) {
	status, err := ReadStatus()
	return status.PID, err
}

// Start launches one detached daemon process unless one is already healthy.
func Start(executable string) (Status, error) {
	if status, err := ReadStatus(); err == nil {
		return status, nil
	}
	cmd := exec.Command(executable, "daemon", "run")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return Status{}, err
	}
	if err := cmd.Process.Release(); err != nil {
		return Status{}, err
	}
	for range 30 {
		time.Sleep(100 * time.Millisecond)
		if status, err := ReadStatus(); err == nil {
			return status, nil
		}
	}
	return Status{}, errors.New("daemon did not become ready")
}

func claim() error {
	path, err := lockPath()
	if err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err == nil {
		return nil
	}
	if _, statusErr := ReadStatus(); statusErr == nil {
		return errors.New("daemon is already running")
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return os.Mkdir(path, 0o700)
}

func release() {
	if path, err := lockPath(); err == nil {
		_ = os.RemoveAll(path)
	}
	if path, err := statusPath(); err == nil {
		_ = os.Remove(path)
	}
}

func writeStatus() error {
	path, err := statusPath()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	status := Status{PID: os.Getpid(), StartedAt: now, Heartbeat: now}
	if old, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(old, &status)
		status.PID = os.Getpid()
		status.Heartbeat = now
	}
	data, _ := json.MarshalIndent(status, "", "  ")
	return state.AtomicWrite(path, append(data, '\n'))
}

func RequestStop() error {
	if _, err := PID(); err != nil {
		return err
	}
	path, err := stopPath()
	if err != nil {
		return err
	}
	return state.AtomicWrite(path, []byte("stop\n"))
}

func statusPath() (string, error) {
	registry, err := state.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(registry), "daemon.json"), nil
}

func lockPath() (string, error) {
	registry, err := state.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(registry), "daemon.lock"), nil
}

func stopPath() (string, error) {
	registry, err := state.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(registry), "daemon.stop"), nil
}

func stopped() bool {
	path, err := stopPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func logf(format string, args ...any) {
	path, err := LogPath()
	if err != nil {
		return
	}
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > 512*1024 {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func LogPath() (string, error) {
	registry, err := state.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(registry), "daemon.log"), nil
}

func Describe() string {
	status, err := ReadStatus()
	if err != nil {
		return "stopped (" + err.Error() + ")"
	}
	return "running (pid " + strconv.Itoa(status.PID) + ", heartbeat " + status.Heartbeat.Local().Format(time.RFC3339) + ")"
}
