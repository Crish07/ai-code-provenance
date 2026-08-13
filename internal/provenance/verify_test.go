// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package provenance_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"ai-prov/internal/app"
	"ai-prov/internal/git"
	provenance "ai-prov/internal/provenance"
	"ai-prov/internal/storage"
)

// TestVerifier_AITrackedLinesFullyCovered walks a session through start→finish
// so the new lines get AI provenance, stages them, and expects coverage=1.
func TestVerifier_AITrackedLinesFullyCovered(t *testing.T) {
	root, store, svc := setupRepoWithSession(t)
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")

	v := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := v.Verify(context.Background(), provenance.Request{Scope: git.ScopeStaged})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.TotalAddedLines != 1 || res.AIAddedLines != 1 || res.UntrackedAddedLines != 0 {
		t.Fatalf("counts got total=%d ai=%d untracked=%d", res.TotalAddedLines, res.AIAddedLines, res.UntrackedAddedLines)
	}
	if res.Coverage != 1 || res.Status != provenance.StatusOK {
		t.Fatalf("got coverage=%v status=%q want 1/ok", res.Coverage, res.Status)
	}
	if len(res.UncoveredFiles) != 0 {
		t.Fatalf("uncovered_files=%v want empty", res.UncoveredFiles)
	}
	if len(res.Sessions) != 1 || res.Sessions[0] != start.SessionID {
		t.Fatalf("sessions=%v want [%s]", res.Sessions, start.SessionID)
	}
	if fr := singleFileReport(t, res.Files, "a.go"); fr != nil {
		if len(fr.AddedLines) != 1 {
			t.Fatalf("file lines=%d want 1", len(fr.AddedLines))
		}
		lr := fr.AddedLines[0]
		if lr.Content != "beta" || lr.Source != provenance.LineSourceAI || lr.SessionID != start.SessionID {
			t.Fatalf("line report=%#v want beta/AI/%s", lr, start.SessionID)
		}
	}
}

// TestVerifier_HumanAddedLinesAreUncovered appends untracked lines after the
// session finishes and expects warning (non-strict) and failed (strict).
func TestVerifier_HumanAddedLinesAreUncovered(t *testing.T) {
	root, store, svc := setupRepoWithSession(t)
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	// Human edit: another line, no session finish records provenance.
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")

	v := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := v.Verify(context.Background(), provenance.Request{Scope: git.ScopeStaged})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.TotalAddedLines != 2 || res.AIAddedLines != 1 || res.UntrackedAddedLines != 1 {
		t.Fatalf("counts got total=%d ai=%d untracked=%d", res.TotalAddedLines, res.AIAddedLines, res.UntrackedAddedLines)
	}
	if res.Coverage != 0.5 {
		t.Fatalf("coverage=%v want 0.5", res.Coverage)
	}
	if res.Status != provenance.StatusWarning {
		t.Fatalf("non-strict status=%q want warning", res.Status)
	}
	if len(res.UncoveredFiles) != 1 || res.UncoveredFiles[0] != "a.go" {
		t.Fatalf("uncovered=%v want [a.go]", res.UncoveredFiles)
	}

	strict, err := v.Verify(context.Background(), provenance.Request{Scope: git.ScopeStaged, Strict: true})
	if err != nil {
		t.Fatalf("Verify strict: %v", err)
	}
	if strict.Status != provenance.StatusFailed {
		t.Fatalf("strict status=%q want failed", strict.Status)
	}
	if fr := singleFileReport(t, res.Files, "a.go"); fr != nil {
		if len(fr.AddedLines) != 2 {
			t.Fatalf("file lines=%d want 2", len(fr.AddedLines))
		}
		if fr.AddedLines[0].Content != "beta" || fr.AddedLines[0].Source != provenance.LineSourceAI {
			t.Fatalf("line 0=%#v want beta/AI", fr.AddedLines[0])
		}
		if fr.AddedLines[1].Content != "gamma" || fr.AddedLines[1].Source != provenance.LineSourceUnknown || fr.AddedLines[1].SessionID != "" {
			t.Fatalf("line 1=%#v want gamma/unknown/empty", fr.AddedLines[1])
		}
	}
}

// TestVerifier_DeleteOnlyDiffHasFullCoverage stages a pure deletion and
// expects zero added lines with coverage pinned to 1.
func TestVerifier_DeleteOnlyDiffHasFullCoverage(t *testing.T) {
	root, store, _ := setupRepoWithSession(t)
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")

	v := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := v.Verify(context.Background(), provenance.Request{Scope: git.ScopeStaged})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.TotalAddedLines != 0 || res.AIAddedLines != 0 || res.UntrackedAddedLines != 0 {
		t.Fatalf("counts got total=%d ai=%d untracked=%d", res.TotalAddedLines, res.AIAddedLines, res.UntrackedAddedLines)
	}
	if res.Coverage != 1 || res.Status != provenance.StatusOK {
		t.Fatalf("got coverage=%v status=%q want 1/ok", res.Coverage, res.Status)
	}
	if len(res.UncoveredFiles) != 0 {
		t.Fatalf("uncovered=%v want empty", res.UncoveredFiles)
	}
	if len(res.Files) != 0 {
		t.Fatalf("delete-only Files=%v want empty", res.Files)
	}
}

// TestVerifier_EmptyDiffIsOK has no staged changes at all.
func TestVerifier_EmptyDiffIsOK(t *testing.T) {
	root, store, _ := setupRepoWithSession(t)
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	v := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := v.Verify(context.Background(), provenance.Request{Scope: git.ScopeStaged, Strict: true})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.TotalAddedLines != 0 || res.Coverage != 1 || res.Status != provenance.StatusOK {
		t.Fatalf("got total=%d coverage=%v status=%q want 0/1/ok", res.TotalAddedLines, res.Coverage, res.Status)
	}
}

// TestVerifier_DuplicateLinesRespectProvenanceCount ensures two identical
// added lines with only one AI row count one covered and one untracked.
func TestVerifier_DuplicateLinesRespectProvenanceCount(t *testing.T) {
	root, store, svc := setupRepoWithSession(t)
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("alpha\ndup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("alpha\ndup\ndup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")

	v := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := v.Verify(context.Background(), provenance.Request{Scope: git.ScopeStaged})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.TotalAddedLines != 2 || res.AIAddedLines != 1 || res.UntrackedAddedLines != 1 {
		t.Fatalf("counts got total=%d ai=%d untracked=%d", res.TotalAddedLines, res.AIAddedLines, res.UntrackedAddedLines)
	}
	if res.Coverage != 0.5 {
		t.Fatalf("coverage=%v want 0.5", res.Coverage)
	}
	if fr := singleFileReport(t, res.Files, "a.go"); fr != nil {
		if len(fr.AddedLines) != 2 {
			t.Fatalf("file lines=%d want 2", len(fr.AddedLines))
		}
		if fr.AddedLines[0].Source != provenance.LineSourceAI || fr.AddedLines[1].Source != provenance.LineSourceUnknown {
			t.Fatalf("dup line sources=%s/%s want AI/unknown", fr.AddedLines[0].Source, fr.AddedLines[1].Source)
		}
	}
}

// TestVerifier_WorktreeScopeMatchesStaged mirrors the staged scenario but
// leaves the changes unstaged and uses the worktree scope.
func TestVerifier_WorktreeScopeMatchesStaged(t *testing.T) {
	root, store, svc := setupRepoWithSession(t)
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	// Leave the modification unstaged so worktree scope reports it.

	v := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := v.Verify(context.Background(), provenance.Request{Scope: git.ScopeWorktree})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Scope != git.ScopeWorktree || res.TotalAddedLines != 1 || res.AIAddedLines != 1 {
		t.Fatalf("got scope=%q total=%d ai=%d", res.Scope, res.TotalAddedLines, res.AIAddedLines)
	}
	if res.Coverage != 1 || res.Status != provenance.StatusOK {
		t.Fatalf("got coverage=%v status=%q want 1/ok", res.Coverage, res.Status)
	}
}

// setupRepoWithSession prepares a git repo and an ai-prov storage + service
// backed by app.Service for the duration of the test.
func setupRepoWithSession(t *testing.T) (root string, store *storage.Store, svc *app.Service) {
	t.Helper()
	root = t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(root, ".ai-provenance"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	store = s
	svc = &app.Service{Root: root, MaxFileBytes: 5 * 1024 * 1024, Store: s}
	return root, store, svc
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

// singleFileReport returns the per-file report for path, or nil if Files is
// empty (a fatal is raised if Files has entries but path is missing).
func singleFileReport(t *testing.T, files []provenance.FileReport, path string) *provenance.FileReport {
	t.Helper()
	if len(files) == 0 {
		return nil
	}
	for i := range files {
		if files[i].Path == path {
			return &files[i]
		}
	}
	t.Fatalf("files=%v want entry for %s", files, path)
	return nil
}
