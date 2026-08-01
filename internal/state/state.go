package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 2

type Registry struct {
	SchemaVersion int                `json:"schema_version"`
	Projects      map[string]Project `json:"projects"`
	LLM           LLMConfig          `json:"llm"`
	LLMUsage      LLMUsage           `json:"llm_usage"`
}

// LLMConfig deliberately contains no secret. API credentials stay in the
// environment so a repository or registry never receives an API key.
type LLMConfig struct {
	Provider          string `json:"provider,omitempty"`
	Model             string `json:"model,omitempty"`
	BaseURL           string `json:"base_url,omitempty"`
	DailyRequestLimit int    `json:"daily_request_limit,omitempty"`
}

type LLMUsage struct {
	Date     string `json:"date,omitempty"`
	Requests int    `json:"requests,omitempty"`
}

type Project struct {
	Name            string    `json:"name"`
	Root            string    `json:"root"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastScanAt      time.Time `json:"last_scan_at,omitempty"`
	Task            *Task     `json:"active_task,omitempty"`
	Enabled         bool      `json:"enabled"`
	LLMAllowed      *bool     `json:"llm_allowed,omitempty"`
	ScanInterval    string    `json:"scan_interval"`
	ScanWindow      string    `json:"scan_window"`
	LastFingerprint string    `json:"last_fingerprint,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	LastHead        string    `json:"last_head,omitempty"`
	LatestChange    *Change   `json:"latest_change,omitempty"`
	CompletedTasks  []Task    `json:"completed_tasks,omitempty"`
	Handoff         *Handoff  `json:"handoff,omitempty"`
}

// Change is the most recent committed transition observed by one-context.
// It remains available after the working tree becomes clean.
type Change struct {
	FromHead   string    `json:"from_head,omitempty"`
	ToHead     string    `json:"to_head"`
	Summary    string    `json:"summary"`
	DiffStat   string    `json:"diff_stat,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

type Task struct {
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Handoff struct {
	Message   string    `json:"message"`
	Tool      string    `json:"tool,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func Path() (string, error) {
	if custom := os.Getenv("ONE_CONTEXT_HOME"); custom != "" {
		return filepath.Join(custom, "registry.json"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "one-context", "registry.json"), nil
}

func Load() (Registry, error) {
	return loadUnlocked()
}

func loadUnlocked() (Registry, error) {
	path, err := Path()
	if err != nil {
		return Registry{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Registry{SchemaVersion: SchemaVersion, Projects: map[string]Project{}}, nil
	}
	if err != nil {
		return Registry{}, err
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return Registry{}, fmt.Errorf("read registry %s: %w", path, err)
	}
	if err := migrateRegistry(&registry); err != nil {
		return Registry{}, err
	}
	if registry.Projects == nil {
		registry.Projects = map[string]Project{}
	}
	for name, project := range registry.Projects {
		if project.ScanInterval == "" {
			project.ScanInterval = "5s"
		}
		if project.ScanWindow == "" {
			project.ScanWindow = "3h"
		}
		registry.Projects[name] = project
	}
	return registry, nil
}

func migrateRegistry(registry *Registry) error {
	switch registry.SchemaVersion {
	case SchemaVersion:
		return nil
	case 1:
		// v2 adds optional LLM usage accounting. Existing configuration and
		// projects remain JSON-compatible, so no data transformation is needed.
		registry.SchemaVersion = SchemaVersion
		return nil
	default:
		return fmt.Errorf("unsupported registry schema %d", registry.SchemaVersion)
	}
}

func Save(registry Registry) error {
	return withLock(func() error { return saveUnlocked(registry) })
}

func saveUnlocked(registry Registry) error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data, 0o600)
}

func Register(name, root string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("project name cannot be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Project{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Project{}, fmt.Errorf("open repository: %w", err)
	}
	if !info.IsDir() {
		return Project{}, fmt.Errorf("repository path is not a directory: %s", abs)
	}
	now := time.Now().UTC()
	project := Project{Name: name, Root: filepath.Clean(abs), CreatedAt: now, UpdatedAt: now, Enabled: true, ScanInterval: "5s", ScanWindow: "3h"}
	err = mutate(func(reg *Registry) error {
		if existing, ok := reg.Projects[name]; ok {
			project.CreatedAt, project.Task, project.CompletedTasks = existing.CreatedAt, existing.Task, existing.CompletedTasks
			project.LastScanAt, project.LastFingerprint, project.LastError = existing.LastScanAt, existing.LastFingerprint, existing.LastError
			project.LastHead, project.LatestChange = existing.LastHead, existing.LatestChange
			project.Handoff = existing.Handoff
			project.LLMAllowed = existing.LLMAllowed
			if existing.ScanInterval != "" {
				project.ScanInterval = existing.ScanInterval
			}
			if existing.ScanWindow != "" {
				project.ScanWindow = existing.ScanWindow
			}
		}
		for otherName, other := range reg.Projects {
			if otherName != name && samePath(other.Root, project.Root) {
				return fmt.Errorf("repository is already registered as %q", otherName)
			}
		}
		reg.Projects[name] = project
		return nil
	})
	return project, err
}

func Resolve(reg Registry, value string) (Project, error) {
	if project, ok := reg.Projects[value]; ok {
		return project, nil
	}
	abs, _ := filepath.Abs(value)
	for _, project := range reg.Projects {
		if samePath(project.Root, abs) {
			return project, nil
		}
	}
	return Project{}, fmt.Errorf("project %q is not registered", value)
}

func Names(reg Registry) []string {
	names := make([]string, 0, len(reg.Projects))
	for name := range reg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Update(project Project) error {
	loadedAt := project.UpdatedAt
	project.UpdatedAt = time.Now().UTC()
	return mutate(func(reg *Registry) error {
		if existing, ok := reg.Projects[project.Name]; ok && existing.UpdatedAt.After(loadedAt) {
			if existing.LastScanAt.After(project.LastScanAt) {
				project.LastScanAt, project.LastFingerprint, project.LastError = existing.LastScanAt, existing.LastFingerprint, existing.LastError
				project.LastHead, project.LatestChange = existing.LastHead, existing.LatestChange
			}
			if existing.Handoff != nil && (project.Handoff == nil || existing.Handoff.UpdatedAt.After(project.Handoff.UpdatedAt)) {
				project.Handoff = existing.Handoff
			}
		}
		reg.Projects[project.Name] = project
		return nil
	})
}

func UpdateLLM(config LLMConfig) error {
	return mutate(func(reg *Registry) error {
		reg.LLM = config
		return nil
	})
}

// ReserveLLMRequest atomically consumes one daily provider request. A zero
// limit means unlimited requests, which is the default for local Ollama.
func ReserveLLMRequest() error {
	return mutate(func(reg *Registry) error {
		limit := reg.LLM.DailyRequestLimit
		if limit <= 0 {
			return nil
		}
		today := time.Now().UTC().Format("2006-01-02")
		if reg.LLMUsage.Date != today {
			reg.LLMUsage = LLMUsage{Date: today}
		}
		if reg.LLMUsage.Requests >= limit {
			return fmt.Errorf("LLM daily request limit reached (%d); raise it with one-context /llm limit <n> or disable the limit with 0", limit)
		}
		reg.LLMUsage.Requests++
		return nil
	})
}

func UpdateScan(name string, scannedAt time.Time, fingerprint, scanError string) error {
	return RecordScan(name, scannedAt, fingerprint, scanError, "", nil)
}

// SetError records a daemon or watcher fault without changing successful scan
// metadata. A later successful scan clears it through RecordScan.
func SetError(name, message string) error {
	return mutate(func(reg *Registry) error {
		project, ok := reg.Projects[name]
		if !ok {
			return fmt.Errorf("project %q is not registered", name)
		}
		project.LastError = message
		project.UpdatedAt = time.Now().UTC()
		reg.Projects[name] = project
		return nil
	})
}

// RecordScan atomically retains the compiler's Git observation with its scan
// metadata, so a daemon refresh cannot lose the last meaningful commit.
func RecordScan(name string, scannedAt time.Time, fingerprint, scanError, head string, latest *Change) error {
	return mutate(func(reg *Registry) error {
		project, ok := reg.Projects[name]
		if !ok {
			return fmt.Errorf("project %q is not registered", name)
		}
		project.LastScanAt, project.LastFingerprint, project.LastError = scannedAt, fingerprint, scanError
		if head != "" {
			project.LastHead = head
		}
		if latest != nil {
			project.LatestChange = latest
		}
		project.UpdatedAt = time.Now().UTC()
		reg.Projects[name] = project
		return nil
	})
}

func Remove(name string) error {
	return mutate(func(reg *Registry) error {
		if _, ok := reg.Projects[name]; !ok {
			return fmt.Errorf("project %q is not registered", name)
		}
		delete(reg.Projects, name)
		return nil
	})
}

func mutate(change func(*Registry) error) error {
	return withLock(func() error {
		reg, err := loadUnlocked()
		if err != nil {
			return err
		}
		if err := change(&reg); err != nil {
			return err
		}
		return saveUnlocked(reg)
	})
}

func withLock(work func() error) error {
	registryPath, err := Path()
	if err != nil {
		return err
	}
	lock := filepath.Join(filepath.Dir(registryPath), "registry.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Mkdir(lock, 0o700)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return err
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.RemoveAll(lock)
			continue
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for registry lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer os.RemoveAll(lock)
	return work()
}

func AtomicWrite(path string, data []byte) error {
	return atomicWrite(path, data, 0o644)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".one-context-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
