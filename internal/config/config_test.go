package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFindProjectRoot(t *testing.T) {
	t.Run("finds git root from nested directory with spaces", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "project with spaces")
		nested := filepath.Join(root, "internal", "deep")
		if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}

		got, err := FindProjectRoot(nested)
		if err != nil {
			t.Fatalf("FindProjectRoot() error = %v", err)
		}
		if got != root {
			t.Errorf("FindProjectRoot() = %q, want %q", got, root)
		}
	})

	t.Run("finds initialized non-git root", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, provenanceDir), 0o755); err != nil {
			t.Fatal(err)
		}

		got, err := FindProjectRoot(root)
		if err != nil {
			t.Fatalf("FindProjectRoot() error = %v", err)
		}
		if got != root {
			t.Errorf("FindProjectRoot() = %q, want %q", got, root)
		}
	})

	t.Run("returns an error outside a project", func(t *testing.T) {
		_, err := FindProjectRoot(t.TempDir())
		if !errors.Is(err, ErrProjectRootNotFound) {
			t.Errorf("FindProjectRoot() error = %v, want ErrProjectRootNotFound", err)
		}
	})
}

func TestConfig_SaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	want := Config{
		SchemaVersion: CurrentSchemaVersion,
		MaxFileBytes:  1024,
		StrictVerify:  true,
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoad_RejectsUnknownOrUnsupportedConfiguration(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, provenanceDir)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, configFile)
	if err := os.WriteFile(path, []byte("schema_version: 2\nmax_file_bytes: 1\nstrict_verify: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("Load() error = nil, want unsupported schema error")
	}

	if err := os.WriteFile(path, []byte("schema_version: 1\nmax_file_bytes: 1\nunknown: value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
}

func TestLoad_MissingConfig(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, ErrProjectNotInitialized) {
		t.Errorf("Load() error = %v, want ErrProjectNotInitialized", err)
	}
}

func TestConfig_HookSectionRoundTrip(t *testing.T) {
	root := t.TempDir()
	hook := &HookConfig{Strict: true, WriteTrailer: false}
	want := Config{
		SchemaVersion: CurrentSchemaVersion,
		MaxFileBytes:  1024,
		StrictVerify:  true,
		Hook:          hook,
	}
	if err := Save(root, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Hook == nil || *got.Hook != *hook {
		t.Errorf("Load().Hook = %v, want %v", got.Hook, hook)
	}
}

func TestConfig_HookSettingsAppliesDefaultsWhenAbsent(t *testing.T) {
	cfg := Config{SchemaVersion: CurrentSchemaVersion, MaxFileBytes: 1, StrictVerify: true}
	got := cfg.HookSettings()
	if !got.Strict || !got.WriteTrailer {
		t.Errorf("HookSettings() = %+v, want {Strict:true WriteTrailer:true}", got)
	}
}

func TestConfig_HookSettingsUsesExplicitValues(t *testing.T) {
	hook := &HookConfig{Strict: false, WriteTrailer: true}
	cfg := Config{SchemaVersion: CurrentSchemaVersion, MaxFileBytes: 1, StrictVerify: true, Hook: hook}
	got := cfg.HookSettings()
	if got.Strict || !got.WriteTrailer {
		t.Errorf("HookSettings() = %+v, want {Strict:false WriteTrailer:true}", got)
	}
}
