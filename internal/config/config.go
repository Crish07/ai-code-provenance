package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// CurrentSchemaVersion is the only configuration version supported by MVP.
	CurrentSchemaVersion = 1
	// DefaultMaxFileBytes limits a tracked text file to five mebibytes.
	DefaultMaxFileBytes int64 = 5 * 1024 * 1024

	provenanceDir = ".ai-provenance"
	configFile    = "config.yaml"
)

var (
	// ErrProjectRootNotFound indicates that no initialized project boundary exists.
	ErrProjectRootNotFound = errors.New("project root not found")
	// ErrProjectNotInitialized indicates that provenance configuration is absent.
	ErrProjectNotInitialized = errors.New("project is not initialized")
)

// Config is the project-local ai-prov configuration.
type Config struct {
	SchemaVersion int         `yaml:"schema_version"`
	MaxFileBytes  int64       `yaml:"max_file_bytes"`
	StrictVerify  bool        `yaml:"strict_verify"`
	Hook          *HookConfig `yaml:"hook,omitempty"`
}

// HookConfig controls the optional commit-msg hook behavior. A nil pointer in
// Config.Hook means "apply defaults" via Config.HookSettings, so a project
// initialized without a hook section still gets sensible behavior once the
// user installs the hook.
type HookConfig struct {
	// Strict aborts the commit when verify finds uncovered added lines.
	Strict bool `yaml:"strict"`
	// WriteTrailer appends AI-Contribution trailers to the commit message.
	WriteTrailer bool `yaml:"write_trailer"`
}

// Default returns a configuration suitable for a newly initialized project.
func Default() Config {
	return Config{
		SchemaVersion: CurrentSchemaVersion,
		MaxFileBytes:  DefaultMaxFileBytes,
		StrictVerify:  false,
	}
}

// HookSettings returns the effective hook settings, applying defaults when the
// configuration does not specify a hook section. When Hook is nil, Strict
// inherits StrictVerify and WriteTrailer defaults to true so installing the
// hook alone is sufficient to record trailers.
func (c Config) HookSettings() HookConfig {
	if c.Hook != nil {
		return *c.Hook
	}
	return HookConfig{Strict: c.StrictVerify, WriteTrailer: true}
}

// FindProjectRoot walks from start upward until it finds .git or
// .ai-provenance. The returned path is absolute and cleaned.
func FindProjectRoot(start string) (string, error) {
	path, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("make project search path absolute: %w", err)
	}

	if info, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("stat project search path: %w", err)
	} else if !info.IsDir() {
		path = filepath.Dir(path)
	}

	for {
		if exists(filepath.Join(path, ".git")) || exists(filepath.Join(path, provenanceDir)) {
			return path, nil
		}

		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("%w: starting at %s", ErrProjectRootNotFound, start)
		}
		path = parent
	}
}

// Load reads and validates the configuration stored beneath root.
func Load(root string) (Config, error) {
	path := filepath.Join(root, provenanceDir, configFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("%w: %s", ErrProjectNotInitialized, root)
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save validates cfg and atomically writes it beneath root.
func Save(root string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	dir := filepath.Join(root, provenanceDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create config temp file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return fmt.Errorf("set config permissions: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tempName, filepath.Join(dir, configFile)); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// Validate rejects configurations that this binary cannot interpret safely.
func (cfg Config) Validate() error {
	if cfg.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported config schema version %d (want %d)", cfg.SchemaVersion, CurrentSchemaVersion)
	}
	if cfg.MaxFileBytes <= 0 {
		return fmt.Errorf("max_file_bytes must be positive")
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
