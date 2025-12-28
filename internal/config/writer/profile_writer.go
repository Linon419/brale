package writer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ProfileYAML represents the structure of profiles.yaml
type ProfileYAML struct {
	Profiles map[string]ProfileEntry `yaml:"profiles"`
}

// ProfileEntry represents a single profile in the YAML file
type ProfileEntry struct {
	ContextTag               string              `yaml:"context_tag,omitempty"`
	Targets                  []string            `yaml:"targets,omitempty"`
	TargetsAPIURL            string              `yaml:"targets_api_url,omitempty"`
	TargetsAPIOverride       bool                `yaml:"targets_api_override,omitempty"`
	TargetsAPIQuote          string              `yaml:"targets_api_quote,omitempty"`
	TargetsAPITimeoutSeconds int                 `yaml:"targets_api_timeout_seconds,omitempty"`
	TargetsAPIRefreshSeconds int                 `yaml:"targets_api_refresh_seconds,omitempty"`
	Intervals                []string            `yaml:"intervals,omitempty"`
	DecisionIntervalMultiple int                 `yaml:"decision_interval_multiple,omitempty"`
	AnalysisSlice            int                 `yaml:"analysis_slice,omitempty"`
	SliceDropTail            int                 `yaml:"slice_drop_tail,omitempty"`
	Middlewares              []MiddlewareEntry   `yaml:"middlewares,omitempty"`
	Prompts                  PromptsEntry        `yaml:"prompts,omitempty"`
	Derivatives              DerivativesEntry    `yaml:"derivatives,omitempty"`
	ExitPlans                ExitPlansEntry      `yaml:"exit_plans,omitempty"`
	Default                  bool                `yaml:"default,omitempty"`
}

type MiddlewareEntry struct {
	Name           string                            `yaml:"name"`
	Stage          int                               `yaml:"stage,omitempty"`
	Critical       bool                              `yaml:"critical,omitempty"`
	TimeoutSeconds int                               `yaml:"timeout_seconds,omitempty"`
	Params         map[string]interface{}            `yaml:"params,omitempty"`
	Configs        map[string]map[string]interface{} `yaml:"configs,omitempty"`
}

type PromptsEntry struct {
	User          string            `yaml:"user,omitempty"`
	SystemByModel map[string]string `yaml:"system_by_model,omitempty"`
}

type DerivativesEntry struct {
	Enabled        bool `yaml:"enabled,omitempty"`
	IncludeOI      bool `yaml:"include_oi,omitempty"`
	IncludeFunding bool `yaml:"include_funding,omitempty"`
}

type ExitPlansEntry struct {
	Combos []string `yaml:"combos,omitempty"`
}

// ProfileWriter handles reading and writing profiles.yaml
type ProfileWriter struct {
	path string
	mu   sync.RWMutex
}

// NewProfileWriter creates a new ProfileWriter for the given path
func NewProfileWriter(path string) *ProfileWriter {
	return &ProfileWriter{path: path}
}

// Read reads the current profiles.yaml content
func (w *ProfileWriter) Read() (*ProfileYAML, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	data, err := os.ReadFile(w.path)
	if err != nil {
		return nil, fmt.Errorf("读取 profiles.yaml 失败: %w", err)
	}

	var cfg ProfileYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析 profiles.yaml 失败: %w", err)
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]ProfileEntry)
	}

	return &cfg, nil
}

// Write writes the profiles to profiles.yaml with backup
func (w *ProfileWriter) Write(cfg *ProfileYAML) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Create backup first
	if err := w.backup(); err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化 profiles 失败: %w", err)
	}

	// Write to temp file first, then rename for atomic write
	tmpPath := w.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	if err := os.Rename(tmpPath, w.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("替换配置文件失败: %w", err)
	}

	return nil
}

// backup creates a backup of the current profiles.yaml
func (w *ProfileWriter) backup() error {
	src, err := os.Open(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file to backup
		}
		return err
	}
	defer src.Close()

	backupDir := filepath.Join(filepath.Dir(w.path), "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("profiles_%s.yaml", timestamp))

	dst, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		return err
	}

	// Clean old backups, keep last 10
	w.cleanOldBackups(backupDir, 10)

	return nil
}

func (w *ProfileWriter) cleanOldBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "profiles_") && strings.HasSuffix(e.Name(), ".yaml") {
			backups = append(backups, filepath.Join(dir, e.Name()))
		}
	}

	if len(backups) <= keep {
		return
	}

	// Remove oldest backups
	for i := 0; i < len(backups)-keep; i++ {
		os.Remove(backups[i])
	}
}

// GetProfile returns a single profile by name
func (w *ProfileWriter) GetProfile(name string) (*ProfileEntry, error) {
	cfg, err := w.Read()
	if err != nil {
		return nil, err
	}

	profile, ok := cfg.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile '%s' 不存在", name)
	}

	return &profile, nil
}

// UpdateProfile updates or creates a profile
func (w *ProfileWriter) UpdateProfile(name string, profile ProfileEntry) error {
	cfg, err := w.Read()
	if err != nil {
		return err
	}

	cfg.Profiles[name] = profile

	return w.Write(cfg)
}

// DeleteProfile deletes a profile by name
func (w *ProfileWriter) DeleteProfile(name string) error {
	cfg, err := w.Read()
	if err != nil {
		return err
	}

	if _, ok := cfg.Profiles[name]; !ok {
		return fmt.Errorf("profile '%s' 不存在", name)
	}

	if len(cfg.Profiles) <= 1 {
		return fmt.Errorf("不能删除唯一的 profile")
	}

	delete(cfg.Profiles, name)

	return w.Write(cfg)
}

// ListPromptFiles lists available prompt files in the prompts directory
func (w *ProfileWriter) ListPromptFiles(promptsDir string) ([]string, error) {
	entries, err := os.ReadDir(promptsDir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			files = append(files, e.Name())
		}
	}

	return files, nil
}

// Path returns the path to profiles.yaml
func (w *ProfileWriter) Path() string {
	return w.path
}
