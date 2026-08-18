// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-prov/internal/app"
	"ai-prov/internal/config"
	"ai-prov/internal/provenance"
	"ai-prov/internal/storage"
)

// chdirTempRepo creates a git repo in a temp dir, chdirs into it, and restores
// the previous working directory on test cleanup. The cobra commands
// (init/status/verify/report) resolve the project root via os.Getwd(), so
// driving them like a real user requires running from inside the repo.
func chdirTempRepo(t *testing.T) string {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "commit.gpgsign", "false")
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return root
}

// TestE2E_InitToStagedVerify_FullCoverage exercises the MVP completion
// scenario from the archived MVP checklist: a user runs `ai-prov init`,
// an agent starts a session, edits a tracked file, finishes the session,
// the user stages the change and runs `ai-prov verify --scope staged --json`.
// The verify result must report status ok with full coverage and the
// contributing session id.
func TestE2E_InitToStagedVerify_FullCoverage(t *testing.T) {
	root := chdirTempRepo(t)
	srcPath := filepath.Join(root, "main.go")
	// Start with a file that already has an empty function body so a single
	// new line is the only diff the agent introduces.
	if err := os.WriteFile(srcPath, []byte("package main\n\nfunc main() {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-m", "init")

	// 1. User runs `ai-prov init` via the real cobra command tree.
	initCmd := NewRootCommand(BuildInfo{Version: "test"})
	initCmd.SetArgs([]string{"init"})
	var initOut bytes.Buffer
	initCmd.SetOut(&initOut)
	initCmd.SetErr(&initOut)
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("ai-prov init: %v (out=%q)", err, initOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".ai-provenance", "config.yaml")); err != nil {
		t.Fatalf("init did not create config.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ai-provenance", "provenance.db")); err != nil {
		t.Fatalf("init did not create provenance.db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ai-provenance", "snapshots")); err != nil {
		t.Fatalf("init did not create snapshots directory: %v", err)
	}
	ignorePath := filepath.Join(root, ".ai-provenance", ".ai-provenanceignore")
	if _, err := os.Stat(ignorePath); err != nil {
		t.Fatalf("init did not create provenance ignore file: %v", err)
	}
	if err := os.WriteFile(ignorePath, []byte("cache/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("repeat ai-prov init: %v", err)
	}
	ignoreContents, err := os.ReadFile(ignorePath)
	if err != nil || string(ignoreContents) != "cache/\n" {
		t.Fatalf("repeat init changed provenance ignore contents=%q err=%v", ignoreContents, err)
	}
	for _, name := range []string{"sessions", "reports"} {
		if _, err := os.Stat(filepath.Join(root, ".ai-provenance", name)); !os.IsNotExist(err) {
			t.Fatalf("init created unused %s directory: err=%v", name, err)
		}
	}

	// 2. Agent (simulated via the app service) starts a session.
	store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}
	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat: greet", Agent: "claude", Model: "gpt-5"})
	if err != nil {
		t.Fatalf("session_start: %v", err)
	}
	if start.State != "active" {
		t.Fatalf("start.State=%q want active", start.State)
	}

	// 3. Agent edits the tracked file (adds exactly one line inside main).
	if err := os.WriteFile(srcPath, []byte("package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. Agent finishes the session, recording the diff as AI provenance.
	finish, err := svc.Finish(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("session_finish: %v", err)
	}
	if finish.State != "finished" || finish.AddedLines != 1 {
		t.Fatalf("finish result = %+v want 1 added line, state finished", finish)
	}

	// 5. User stages the change.
	runGit(t, root, "add", "main.go")

	// 6. User runs `ai-prov verify --scope staged --json` via the real cobra tree.
	verifyCmd := NewRootCommand(BuildInfo{Version: "test"})
	verifyCmd.SetArgs([]string{"verify", "--scope", "staged", "--json"})
	var verifyOut, verifyErr bytes.Buffer
	verifyCmd.SetOut(&verifyOut)
	verifyCmd.SetErr(&verifyErr)
	if err := verifyCmd.Execute(); err != nil {
		t.Fatalf("ai-prov verify: %v (stderr=%q)", err, verifyErr.String())
	}

	// 7. Assert the JSON output matches the API contract: status ok, coverage 1,
	// sessions populated, no uncovered files.
	var got verifyOutput
	if err := json.Unmarshal(bytes.TrimSpace(verifyOut.Bytes()), &got); err != nil {
		t.Fatalf("unmarshal verify json: %v (raw=%q)", err, verifyOut.String())
	}
	if got.Status != string(provenance.StatusOK) {
		t.Errorf("status=%q want ok", got.Status)
	}
	if got.Scope != "staged" {
		t.Errorf("scope=%q want staged", got.Scope)
	}
	if got.TotalAddedLines != 1 || got.AIAddedLines != 1 || got.UntrackedAddedLines != 0 {
		t.Errorf("line counts = total=%d ai=%d untracked=%d, want 1/1/0", got.TotalAddedLines, got.AIAddedLines, got.UntrackedAddedLines)
	}
	if got.Coverage != 1 {
		t.Errorf("coverage=%v want 1", got.Coverage)
	}
	if len(got.Sessions) != 1 || !strings.HasPrefix(got.Sessions[0], start.SessionID[:8]) {
		t.Errorf("sessions=%v want [%s...]", got.Sessions, start.SessionID[:8])
	}
	if len(got.UncoveredFiles) != 0 {
		t.Errorf("uncovered_files=%v want empty", got.UncoveredFiles)
	}
}

// TestE2E_StrictVerifyExitsNonZeroOnUntrackedLines confirms the strict path
// surfaces as a cobra error so the CLI exits 1, matching the API contract for
// status failed.
func TestE2E_StrictVerifyExitsNonZeroOnUntrackedLines(t *testing.T) {
	root := chdirTempRepo(t)
	srcPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(srcPath, []byte("package main\n\nfunc main() {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-m", "init")

	// init via cobra
	initCmd := NewRootCommand(BuildInfo{Version: "test"})
	initCmd.SetArgs([]string{"init"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("ai-prov init: %v", err)
	}

	// session start + finish for one AI line
	store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}
	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat", Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcPath, []byte("package main\n\nfunc main() {\n\tprintln(\"ai\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}

	// Add a human (uncovered) line after the session was finished, then stage.
	if err := os.WriteFile(srcPath, []byte("package main\n\nfunc main() {\n\tprintln(\"ai\")\n\tprintln(\"human\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "main.go")

	// verify --strict must surface status failed via a cobra error. The JSON
	// report is written to stdout before the error is returned; the error text
	// goes to stderr, so the two stay separable.
	verifyCmd := NewRootCommand(BuildInfo{Version: "test"})
	verifyCmd.SetArgs([]string{"verify", "--scope", "staged", "--strict", "--json"})
	var verifyOut, verifyErr bytes.Buffer
	verifyCmd.SetOut(&verifyOut)
	verifyCmd.SetErr(&verifyErr)
	err = verifyCmd.Execute()
	if err == nil {
		t.Fatalf("verify --strict on untracked lines: want error, got nil (out=%q)", verifyOut.String())
	}
	var got verifyOutput
	if err := json.Unmarshal(bytes.TrimSpace(verifyOut.Bytes()), &got); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q stderr=%q)", err, verifyOut.String(), verifyErr.String())
	}
	if got.Status != string(provenance.StatusFailed) {
		t.Errorf("status=%q want failed", got.Status)
	}
	if got.UntrackedAddedLines != 1 {
		t.Errorf("untracked=%d want 1", got.UntrackedAddedLines)
	}
}

// TestE2E_VersionReflectsInjectedBuildInfo confirms ldflags-injected version
// metadata surfaces in `ai-prov version` so the release target can be audited.
func TestE2E_VersionReflectsInjectedBuildInfo(t *testing.T) {
	cmd := NewRootCommand(BuildInfo{Version: "v0.1.0", Commit: "abc123", BuiltAt: "2026-08-04T00:00:00Z"})
	cmd.SetArgs([]string{"version"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	for _, want := range []string{"v0.1.0", "abc123", "2026-08-04T00:00:00Z"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("version output missing %q:\n%s", want, out.String())
		}
	}
}

// TestE2E_StatusReportsProjectState confirms `ai-prov status` runs against an
// initialized project and reports session counts.
func TestE2E_StatusReportsProjectState(t *testing.T) {
	root := chdirTempRepo(t)
	srcPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(srcPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-m", "init")

	// init via cobra
	initCmd := NewRootCommand(BuildInfo{Version: "test"})
	initCmd.SetArgs([]string{"init"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("ai-prov init: %v", err)
	}

	// status before any session
	statusCmd := NewRootCommand(BuildInfo{Version: "test"})
	statusCmd.SetArgs([]string{"status"})
	var out bytes.Buffer
	statusCmd.SetOut(&out)
	statusCmd.SetErr(&out)
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("ai-prov status: %v (out=%q)", err, out.String())
	}
	if !strings.Contains(out.String(), "active=0 finished=0 failed=0") {
		t.Errorf("status output=%q want zero counts", out.String())
	}
}

// runGit is shared with the rest of the cli package (see verify_test.go).
