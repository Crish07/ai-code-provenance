package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ai-prov/internal/app"
	"ai-prov/internal/config"
	"ai-prov/internal/provenance"
	"ai-prov/internal/storage"
)

// TestRunVerify_AITrackedLinesExitOK stages a session-finished line and
// expects status ok with no error (cobra wrapper would exit 0).
func TestRunVerify_AITrackedLinesExitOK(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")

	var out bytes.Buffer
	status, err := RunVerify(&out, root, "staged", false, false)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if status != provenance.StatusOK {
		t.Fatalf("status=%q want ok", status)
	}
	if !strings.Contains(out.String(), "status:   ok") {
		t.Fatalf("terminal output=%q want status ok line", out.String())
	}
}

// TestRunVerify_HumanAddedLinesAreWarning stages an AI line plus a human line
// and expects status warning (non-strict) with no infrastructure error.
func TestRunVerify_HumanAddedLinesAreWarning(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\ngamma\n")
	runGit(t, root, "add", "a.go")

	var out bytes.Buffer
	status, err := RunVerify(&out, root, "staged", false, false)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if status != provenance.StatusWarning {
		t.Fatalf("status=%q want warning", status)
	}
}

// TestRunVerify_StrictStatusIsFailed verifies the strict flag flips the status
// to failed for uncovered lines.
func TestRunVerify_StrictStatusIsFailed(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\ngamma\n")
	runGit(t, root, "add", "a.go")

	var out bytes.Buffer
	status, err := RunVerify(&out, root, "staged", true, false)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if status != provenance.StatusFailed {
		t.Fatalf("status=%q want failed", status)
	}
}

// TestRunVerify_DeleteOnlyDiffIsOK stages a pure deletion and expects ok.
func TestRunVerify_DeleteOnlyDiffIsOK(t *testing.T) {
	root, _, _ := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\nbeta\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")

	var out bytes.Buffer
	status, err := RunVerify(&out, root, "staged", true, false)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if status != provenance.StatusOK {
		t.Fatalf("status=%q want ok", status)
	}
}

// TestRunVerify_EmptyDiffIsOK verifies a clean index reports ok.
func TestRunVerify_EmptyDiffIsOK(t *testing.T) {
	root, _, _ := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	var out bytes.Buffer
	status, err := RunVerify(&out, root, "staged", true, false)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if status != provenance.StatusOK {
		t.Fatalf("status=%q want ok", status)
	}
}

// TestRunVerify_JSONOutputShape checks the JSON payload fields and that an
// uncovered warning is still serialized (exit handled by the cobra wrapper).
func TestRunVerify_JSONOutputShape(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\ngamma\n")
	runGit(t, root, "add", "a.go")

	var out bytes.Buffer
	status, err := RunVerify(&out, root, "staged", false, true)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if status != provenance.StatusWarning {
		t.Fatalf("status=%q want warning", status)
	}
	var got verifyOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out.String())
	}
	if got.Status != "warning" || got.Scope != "staged" {
		t.Fatalf("json=%#v", got)
	}
	if got.TotalAddedLines != 2 || got.AIAddedLines != 1 || got.UntrackedAddedLines != 1 {
		t.Fatalf("counts=%#v", got)
	}
	if got.Coverage != 0.5 {
		t.Fatalf("coverage=%v want 0.5", got.Coverage)
	}
	if len(got.Sessions) != 1 || got.Sessions[0] != start.SessionID {
		t.Fatalf("sessions=%v want [%s]", got.Sessions, start.SessionID)
	}
	if len(got.UncoveredFiles) != 1 || got.UncoveredFiles[0] != "a.go" {
		t.Fatalf("uncovered_files=%v want [a.go]", got.UncoveredFiles)
	}
}

// TestRunVerify_InvalidScopeReturnsError ensures a bad scope is rejected
// before reaching git.
func TestRunVerify_InvalidScopeReturnsError(t *testing.T) {
	root, _, _ := setupCLIRepo(t)
	var out bytes.Buffer
	if _, err := RunVerify(&out, root, "bogus", false, false); err == nil {
		t.Fatal("RunVerify bogus scope: want error, got nil")
	}
}

// TestRunVerify_CommandExitsNonOKOnWarning drives the cobra wrapper (which
// resolves root via os.Getwd) to confirm a warning status surfaces as
// errVerifyNonOK (exit 1), while ok returns nil.
func TestRunVerify_CommandExitsNonOKOnWarning(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\ngamma\n")
	runGit(t, root, "add", "a.go")

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	var out bytes.Buffer
	c := newVerifyCommand()
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs([]string{"--scope", "staged"})
	if err := c.Execute(); !errors.Is(err, errVerifyNonOK) {
		t.Fatalf("warning Execute err=%v, want errVerifyNonOK", err)
	}

	// ok case: a clean repo with no staged changes.
	root2, _, _ := setupCLIRepo(t)
	p2 := filepath.Join(root2, "a.go")
	writeFile(t, p2, "alpha\n")
	runGit(t, root2, "add", "a.go")
	runGit(t, root2, "commit", "-m", "init")
	if err := os.Chdir(root2); err != nil {
		t.Fatal(err)
	}
	c2 := newVerifyCommand()
	var out2 bytes.Buffer
	c2.SetOut(&out2)
	c2.SetArgs([]string{"--scope", "staged"})
	if err := c2.Execute(); err != nil {
		t.Fatalf("ok Execute err=%v, want nil", err)
	}
}

func setupCLIRepo(t *testing.T) (root string, store *storage.Store, svc *app.Service) {
	t.Helper()
	root = t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(root, ".ai-provenance"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, config.Default()); err != nil {
		t.Fatal(err)
	}
	s, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	store = s
	svc = &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: s}
	return root, store, svc
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	var errBuf bytes.Buffer
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v in %s: %v %s", args, root, err, errBuf.String())
	}
}
