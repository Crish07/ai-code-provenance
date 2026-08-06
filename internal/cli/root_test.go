package cli

import (
	"bytes"
	"testing"
)

func TestNewRootCommand_Version(t *testing.T) {
	root := NewRootCommand(BuildInfo{
		Version: "v0.1.0",
		Commit:  "abc123",
		BuiltAt: "2026-08-04T00:00:00Z",
	})

	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	const want = "ai-prov v0.1.0\ncommit: abc123\nbuilt at: 2026-08-04T00:00:00Z\n"
	if output.String() != want {
		t.Errorf("version output = %q, want %q", output.String(), want)
	}
}

func TestNewRootCommand_VersionRejectsArguments(t *testing.T) {
	root := NewRootCommand(BuildInfo{})
	root.SetArgs([]string{"version", "extra"})

	if err := root.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want argument validation error")
	}
}
