package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-prov/internal/app"
	"ai-prov/internal/config"
	"ai-prov/internal/git"
	"ai-prov/internal/provenance"
)

// ---------------------------------------------------------------------------
// Pure trailer rendering
// ---------------------------------------------------------------------------

func TestRenderTrailer_EmptyDiffReturnsEmpty(t *testing.T) {
	res := provenance.Result{Status: provenance.StatusOK, Scope: git.ScopeStaged, Coverage: 1, Sessions: []string{}}
	if got := RenderTrailer(res, ""); got != "" {
		t.Errorf("RenderTrailer empty diff = %q, want empty", got)
	}
}

func TestRenderTrailer_FullCoverageIncludesAllKeys(t *testing.T) {
	res := provenance.Result{
		Status:          provenance.StatusOK,
		Scope:           git.ScopeStaged,
		TotalAddedLines: 5,
		AIAddedLines:    5,
		Coverage:        1,
		Sessions:        []string{"abcdef12-3456-7890-abcd-ef1234567890"},
	}
	got := RenderTrailer(res, "claude")
	for _, want := range []string{
		"AI-Contribution: 100%",
		"AI-Lines: 5/5",
		"AI-Agent: claude",
		"AI-Confidence: 100%",
		"AI-Provenance-ID: abcdef12",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderTrailer missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderTrailer_FiftyPercentCoverageRoundsDown(t *testing.T) {
	res := provenance.Result{
		TotalAddedLines: 2,
		AIAddedLines:    1,
		Coverage:        0.5,
		Sessions:        []string{"abc12345"},
	}
	got := RenderTrailer(res, "")
	if !strings.Contains(got, "AI-Contribution: 50%") {
		t.Errorf("expected 50%%, got:\n%s", got)
	}
	if !strings.Contains(got, "AI-Agent: unknown") {
		t.Errorf("empty agent should default to unknown, got:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// Commit message rewrite
// ---------------------------------------------------------------------------

func TestRewriteCommitMessage_PreservesForeignTrailers(t *testing.T) {
	msg := "feat: add thing\n\nBody line.\n\nSigned-off-by: A <a@example.com>\n"
	trailer := "AI-Contribution: 100%\nAI-Lines: 1/1\n"
	got := RewriteCommitMessage(msg, trailer)
	if !strings.Contains(got, "Signed-off-by: A <a@example.com>") {
		t.Errorf("foreign trailer dropped:\n%s", got)
	}
	if !strings.Contains(got, "AI-Contribution: 100%") {
		t.Errorf("AI trailer missing:\n%s", got)
	}
}

func TestRewriteCommitMessage_ReplacesPriorAIPrivTrailers(t *testing.T) {
	msg := "feat: add thing\n\nAI-Contribution: 10%\nAI-Lines: 1/10\n\nSigned-off-by: A\n"
	trailer := "AI-Contribution: 100%\nAI-Lines: 1/1\n"
	got := RewriteCommitMessage(msg, trailer)
	count := strings.Count(got, "AI-Contribution:")
	if count != 1 {
		t.Errorf("AI-Contribution occurrences = %d, want 1:\n%s", count, got)
	}
	if !strings.Contains(got, "AI-Contribution: 100%") {
		t.Errorf("expected fresh 100%% trailer:\n%s", got)
	}
	if !strings.Contains(got, "Signed-off-by: A") {
		t.Errorf("foreign trailer dropped:\n%s", got)
	}
}

func TestRewriteCommitMessage_StripsStaleTrailersWhenTrailerEmpty(t *testing.T) {
	msg := "feat: add thing\n\nAI-Contribution: 50%\nAI-Lines: 1/2\n"
	got := RewriteCommitMessage(msg, "")
	if strings.Contains(got, "AI-Contribution") {
		t.Errorf("stale trailer not stripped:\n%s", got)
	}
	if !strings.HasPrefix(got, "feat: add thing") {
		t.Errorf("body corrupted:\n%s", got)
	}
}

func TestRewriteCommitMessage_NoTrailersAppendsBlock(t *testing.T) {
	msg := "feat: add thing\n"
	trailer := "AI-Contribution: 100%\n"
	got := RewriteCommitMessage(msg, trailer)
	if !strings.HasPrefix(got, "feat: add thing\n\nAI-Contribution: 100%\n") {
		t.Errorf("unexpected assembly:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// RunCommitMsgHook end-to-end with a temp git repo and a finished session
// ---------------------------------------------------------------------------

// TestRunCommitMsgHook_WritesTrailerOnAICommit stages an AI-finished diff and
// expects the commit message file to receive a 100% AI-Contribution trailer
// while preserving a pre-existing Signed-off-by trailer.
func TestRunCommitMsgHook_WritesTrailerOnAICommit(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat", Agent: "claude", Model: "gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")

	msgFile := filepath.Join(root, "MSG")
	writeFile(t, msgFile, "feat: add beta\n\nSigned-off-by: dev <dev@example.com>\n")

	var errOut bytes.Buffer
	if err := RunCommitMsgHook(&errOut, root, msgFile); err != nil {
		t.Fatalf("RunCommitMsgHook: %v (stderr=%q)", err, errOut.String())
	}
	got, err := os.ReadFile(msgFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"AI-Contribution: 100%",
		"AI-Lines: 1/1",
		"AI-Agent: claude",
		"AI-Confidence: 100%",
		"Signed-off-by: dev <dev@example.com>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("commit message missing %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "AI-Contribution:") != 1 {
		t.Errorf("AI-Contribution should appear once:\n%s", s)
	}
}

// TestRunCommitMsgHook_StrictAbortsOnUntrackedLines configures strict mode and
// stages an AI line plus a human line; the hook must return errHookBlocked and
// still write a trailer reflecting the partial coverage.
func TestRunCommitMsgHook_StrictAbortsOnUntrackedLines(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	// Enable strict via explicit hook config.
	if err := config.Save(root, config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		MaxFileBytes:  config.DefaultMaxFileBytes,
		Hook:          &config.HookConfig{Strict: true, WriteTrailer: true},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat", Agent: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\ngamma\n") // gamma is human-added, uncovered
	runGit(t, root, "add", "a.go")

	msgFile := filepath.Join(root, "MSG")
	writeFile(t, msgFile, "feat: mixed\n")

	var errOut bytes.Buffer
	err = RunCommitMsgHook(&errOut, root, msgFile)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("err = %v, want errHookBlocked", err)
	}
	if !strings.Contains(errOut.String(), "commit blocked") {
		t.Errorf("stderr missing diagnostic:\n%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "a.go") {
		t.Errorf("stderr missing uncovered file name:\n%s", errOut.String())
	}
	got, _ := os.ReadFile(msgFile)
	if !strings.Contains(string(got), "AI-Contribution: 50%") {
		t.Errorf("trailer should still record partial coverage:\n%s", got)
	}
}

// TestRunCommitMsgHook_EmptyDiffWritesNoTrailer verifies a commit with no
// staged code changes leaves the message untouched.
func TestRunCommitMsgHook_EmptyDiffWritesNoTrailer(t *testing.T) {
	root, _, _ := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	original := "feat: nothing staged\n"
	msgFile := filepath.Join(root, "MSG")
	writeFile(t, msgFile, original)

	var errOut bytes.Buffer
	if err := RunCommitMsgHook(&errOut, root, msgFile); err != nil {
		t.Fatalf("RunCommitMsgHook: %v", err)
	}
	got, _ := os.ReadFile(msgFile)
	if string(got) != original {
		t.Errorf("empty diff should not modify message:\ngot=%q\nwant=%q", got, original)
	}
}

// TestRunCommitMsgHook_WriteTrailerDisabledSkipsTrailer verifies that when
// the project config disables trailer writing, the hook still runs verify but
// leaves the commit message untouched.
func TestRunCommitMsgHook_WriteTrailerDisabledSkipsTrailer(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	if err := config.Save(root, config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		MaxFileBytes:  config.DefaultMaxFileBytes,
		Hook:          &config.HookConfig{Strict: false, WriteTrailer: false},
	}); err != nil {
		t.Fatal(err)
	}
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

	original := "feat: trailer disabled\n"
	msgFile := filepath.Join(root, "MSG")
	writeFile(t, msgFile, original)

	var errOut bytes.Buffer
	if err := RunCommitMsgHook(&errOut, root, msgFile); err != nil {
		t.Fatalf("RunCommitMsgHook: %v", err)
	}
	got, _ := os.ReadFile(msgFile)
	if string(got) != original {
		t.Errorf("trailer should not be written when write_trailer=false:\ngot=%q", got)
	}
}

// TestRunCommitMsgHook_AmendReplacesPriorTrailer stages an AI diff, runs the
// hook once, then re-runs the hook on the message that now carries our trailer
// (simulating a `git commit --amend` re-invocation). The trailer block must be
// replaced rather than duplicated.
func TestRunCommitMsgHook_AmendReplacesPriorTrailer(t *testing.T) {
	root, _, svc := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat", Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")

	msgFile := filepath.Join(root, "MSG")
	writeFile(t, msgFile, "feat: amend me\n")
	var errOut bytes.Buffer
	if err := RunCommitMsgHook(&errOut, root, msgFile); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first, _ := os.ReadFile(msgFile)
	if !strings.Contains(string(first), "AI-Contribution: 100%") {
		t.Fatalf("first run missing trailer:\n%s", first)
	}

	// Re-run on the message that already carries our trailer (amend).
	if err := RunCommitMsgHook(&errOut, root, msgFile); err != nil {
		t.Fatalf("amend run: %v", err)
	}
	got, _ := os.ReadFile(msgFile)
	if c := strings.Count(string(got), "AI-Contribution:"); c != 1 {
		t.Errorf("amend should not duplicate trailer (count=%d):\n%s", c, got)
	}
	if c := strings.Count(string(got), "AI-Agent:"); c != 1 {
		t.Errorf("amend should not duplicate AI-Agent (count=%d):\n%s", c, got)
	}
	if !strings.HasPrefix(string(got), "feat: amend me\n") {
		t.Errorf("body corrupted by amend:\n%s", got)
	}
}

// TestNewHookCommand_HelpListsSubcommands confirms install/uninstall are
// visible (run is hidden).
func TestNewHookCommand_HelpListsSubcommands(t *testing.T) {
	cmd := newHookCommand()
	names := map[string]bool{}
	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}
		names[sub.Name()] = true
	}
	for _, want := range []string{"install", "uninstall"} {
		if !names[want] {
			t.Errorf("subcommand %q missing from hook tree: %v", want, names)
		}
	}
	if names["run"] {
		t.Errorf("run subcommand should be hidden but is visible")
	}
}
