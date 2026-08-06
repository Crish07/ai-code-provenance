package cli

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteDebugBundle_SanitizedRuntimeMetadataOnly(t *testing.T) {
	output := filepath.Join(t.TempDir(), "debug.zip")
	path, err := writeDebugBundle(output, BuildInfo{
		Version: "v0.2.0",
		Commit:  "abc123",
		BuiltAt: "2026-08-06T00:00:00Z",
	}, time.Date(2026, 8, 6, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatalf("writeDebugBundle() error = %v", err)
	}
	if path != output {
		t.Errorf("bundle path = %q, want %q", path, output)
	}

	archive, err := zip.OpenReader(output)
	if err != nil {
		t.Fatalf("open debug bundle: %v", err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "diagnostics.json" {
		t.Fatalf("bundle files = %v, want only diagnostics.json", zipNames(archive.File))
	}

	file, err := archive.File[0].Open()
	if err != nil {
		t.Fatalf("open diagnostics: %v", err)
	}
	defer file.Close()
	var got debugBundle
	if err := json.NewDecoder(file).Decode(&got); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	if got.Format != "ai-prov-debug-v1" || got.Version != "v0.2.0" || got.Commit != "abc123" {
		t.Errorf("diagnostics = %#v, want injected runtime metadata", got)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read debug bundle: %v", err)
	}
	for _, forbidden := range []string{".ai-provenance", "snapshot", "diff", "token", "credential", ".git/config"} {
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Errorf("debug bundle contains forbidden marker %q", forbidden)
		}
	}
}

func TestWriteDebugBundle_RejectsNonZipOutput(t *testing.T) {
	_, err := writeDebugBundle(filepath.Join(t.TempDir(), "debug.txt"), BuildInfo{}, time.Now())
	if err == nil {
		t.Fatal("writeDebugBundle() error = nil, want extension validation error")
	}
}

func TestNewDebugCommand_BundleWritesSanitizedArchive(t *testing.T) {
	output := filepath.Join(t.TempDir(), "debug.zip")
	root := NewRootCommand(BuildInfo{Version: "v0.2.0"})
	root.SetArgs([]string{"debug", "bundle", "--output", output})
	if err := root.Execute(); err != nil {
		t.Fatalf("debug bundle command error = %v", err)
	}
	archive, err := zip.OpenReader(output)
	if err != nil {
		t.Fatalf("open command bundle: %v", err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "diagnostics.json" {
		t.Errorf("command bundle files = %v, want only diagnostics.json", zipNames(archive.File))
	}
}

func zipNames(files []*zip.File) []string {
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = file.Name
	}
	return names
}
