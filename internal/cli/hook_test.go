// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

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
	if got := RenderTrailer(res, "", config.Default().HookSettings()); got != "" {
		t.Errorf("RenderTrailer empty diff = %q, want empty", got)
	}
}

func TestRenderTrailer_DefaultIncludesOnlyLinesAndAgent(t *testing.T) {
	res := provenance.Result{
		Status:          provenance.StatusOK,
		Scope:           git.ScopeStaged,
		TotalAddedLines: 5,
		AIAddedLines:    5,
		Coverage:        1,
		Sessions:        []string{"abcdef12-3456-7890-abcd-ef1234567890"},
	}
	got := RenderTrailer(res, "claude", config.Default().HookSettings())
	for _, want := range []string{
		"AI-Lines: 5/5",
		"AI-Agent: claude",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderTrailer missing %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"AI-Contribution:", "AI-Confidence:", "AI-Provenance-ID:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("default trailer unexpectedly contains %q:\n%s", unwanted, got)
		}
	}
	if strings.Contains(got, trailerCommentMark) {
		t.Errorf("default trailer unexpectedly contains comment marker:\n%s", got)
	}
}

func TestRenderTrailer_OptionalFieldsAndCommentToggle(t *testing.T) {
	comments := false
	settings := config.HookConfig{Trailer: &config.TrailerConfig{Fields: []string{"lines", "provenance-id", "agent"}, Comments: &comments}}
	res := provenance.Result{TotalAddedLines: 2, AIAddedLines: 1, Coverage: .5, Sessions: []string{"abcdef12-3456"}}
	got := RenderTrailer(res, "codex", settings)
	for _, want := range []string{"AI-Lines: 1/2", "AI-Provenance-ID: abcdef12", "AI-Agent: codex"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "# ai-prov trailer") || strings.Contains(got, "AI-Contribution:") {
		t.Errorf("unexpected optional output: %s", got)
	}
}

func TestRenderTrailer_CoverageIsOnlyRenderedWhenConfigured(t *testing.T) {
	res := provenance.Result{
		TotalAddedLines: 2,
		AIAddedLines:    1,
		Coverage:        0.5,
		Sessions:        []string{"abc12345"},
	}
	comments := false
	got := RenderTrailer(res, "", config.HookConfig{Trailer: &config.TrailerConfig{Fields: []string{"coverage", "agent"}, Comments: &comments}})
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
		"AI-Lines: 1/1",
		"AI-Agent: claude",
		"Signed-off-by: dev <dev@example.com>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("commit message missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "AI-Confidence:") || strings.Contains(s, "AI-Contribution:") {
		t.Errorf("default trailer contains disabled field:\n%s", s)
	}
	if strings.Contains(s, trailerCommentMark) {
		t.Errorf("default trailer contains comment marker:\n%s", s)
	}
	if strings.Count(s, "[AI:100%]") != 1 {
		t.Errorf("title coverage should appear once:\n%s", s)
	}
	if !strings.HasPrefix(s, "feat: add beta [AI:100%]\n") {
		t.Errorf("title coverage missing:\n%s", s)
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
	if !strings.Contains(string(got), "feat: mixed [AI:50%]") {
		t.Errorf("title should still record partial coverage:\n%s", got)
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

	original := "feat: trailer disabled\n\n# ai-prov trailer\nAI-Contribution: 100%\nAI-Confidence: 100%\n"
	msgFile := filepath.Join(root, "MSG")
	writeFile(t, msgFile, original)

	var errOut bytes.Buffer
	if err := RunCommitMsgHook(&errOut, root, msgFile); err != nil {
		t.Fatalf("RunCommitMsgHook: %v", err)
	}
	got, _ := os.ReadFile(msgFile)
	if strings.Contains(string(got), "AI-Contribution:") || strings.Contains(string(got), "AI-Confidence:") || strings.Contains(string(got), trailerCommentMark) {
		t.Errorf("disabled trailer should remove stale ai-prov fields:\ngot=%q", got)
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
	if !strings.Contains(string(first), "feat: amend me [AI:100%]") {
		t.Fatalf("first run missing title coverage:\n%s", first)
	}

	// Re-run on the message that already carries our trailer (amend).
	if err := RunCommitMsgHook(&errOut, root, msgFile); err != nil {
		t.Fatalf("amend run: %v", err)
	}
	got, _ := os.ReadFile(msgFile)
	if c := strings.Count(string(got), "[AI:100%]"); c != 1 {
		t.Errorf("amend should not duplicate title coverage (count=%d):\n%s", c, got)
	}
	if c := strings.Count(string(got), "AI-Agent:"); c != 1 {
		t.Errorf("amend should not duplicate AI-Agent (count=%d):\n%s", c, got)
	}
	if !strings.HasPrefix(string(got), "feat: amend me [AI:100%]\n") {
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
	for _, want := range []string{"install", "uninstall", "config"} {
		if !names[want] {
			t.Errorf("subcommand %q missing from hook tree: %v", want, names)
		}
	}
	if names["run"] {
		t.Errorf("run subcommand should be hidden but is visible")
	}
}

func TestHookConfig_SetShowReset(t *testing.T) {
	root, _, _ := setupCLIRepo(t)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cmd := newHookConfigSetCommand()
	cmd.SetArgs([]string{"--fields", "lines,agent", "--comments=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.HookSettings()
	if strings.Join(settings.Trailer.Fields, ",") != "lines,agent" || *settings.Trailer.Comments {
		t.Fatalf("settings=%#v", settings)
	}
	show := newHookConfigShowCommand()
	var output bytes.Buffer
	show.SetOut(&output)
	if err := show.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "字段说明") || !strings.Contains(output.String(), "lines,agent") {
		t.Fatalf("show=%s", output.String())
	}
	reset := newHookConfigResetCommand()
	if err := reset.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	settings = cfg.HookSettings()
	if strings.Join(settings.Trailer.Fields, ",") != "lines,agent" || *settings.Trailer.Comments || settings.TitleCoverage == nil || !*settings.TitleCoverage {
		t.Fatalf("reset=%#v", settings)
	}
}

func TestRewriteCommitMessageWithCoverage_UpdatesOrRemovesTitleSuffix(t *testing.T) {
	res := provenance.Result{TotalAddedLines: 2, AIAddedLines: 1, Coverage: .5}
	got := RewriteCommitMessageWithCoverage("feat: title [AI:100%]\n\nBody\n", "AI-Lines: 1/2\n", res, true)
	if !strings.HasPrefix(got, "feat: title [AI:50%]\n") || strings.Count(got, "[AI:") != 1 {
		t.Fatalf("updated title = %q", got)
	}
	got = RewriteCommitMessageWithCoverage(got, "AI-Contribution: 50%\n", res, false)
	if strings.Contains(got, "[AI:") || !strings.Contains(got, "AI-Contribution: 50%") {
		t.Fatalf("trailer-only switch = %q", got)
	}
	got = RewriteCommitMessageWithCoverage("feat: empty [AI:50%]\n", "", provenance.Result{}, true)
	if strings.Contains(got, "[AI:") {
		t.Fatalf("empty diff retained suffix: %q", got)
	}
}

func TestRenderTrailer_ExplicitCommentsWritesMarker(t *testing.T) {
	yes := true
	res := provenance.Result{TotalAddedLines: 1, AIAddedLines: 1, Coverage: 1}
	got := RenderTrailer(res, "codex", config.HookConfig{Trailer: &config.TrailerConfig{Fields: []string{"lines", "agent"}, Comments: &yes}})
	if !strings.HasPrefix(got, trailerCommentMark+"\n") {
		t.Fatalf("explicit comments did not write marker: %q", got)
	}
}

func TestRewriteCommitMessage_RemovesLegacyCommentMarker(t *testing.T) {
	msg := "feat: legacy\n\n# ai-prov trailer\nAI-Lines: 1/1\n\nSigned-off-by: A\n"
	got := RewriteCommitMessage(msg, "AI-Lines: 2/2\n")
	if strings.Contains(got, trailerCommentMark) || !strings.Contains(got, "Signed-off-by: A") {
		t.Fatalf("legacy marker cleanup corrupted foreign trailer: %q", got)
	}
}

func TestSaveHookInstallDefaults_SelectsModeAndTrailerFields(t *testing.T) {
	root, _, _ := setupCLIRepo(t)
	if err := saveHookInstallDefaults(root, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.HookSettings()
	if settings.TitleCoverage == nil || !*settings.TitleCoverage || strings.Join(settings.Trailer.Fields, ",") != "lines,agent" {
		t.Fatalf("default install settings=%#v", settings)
	}
	if err := saveHookInstallDefaults(root, true); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	settings = cfg.HookSettings()
	if settings.TitleCoverage == nil || *settings.TitleCoverage || strings.Join(settings.Trailer.Fields, ",") != "coverage,lines,agent" {
		t.Fatalf("trailer-only settings=%#v", settings)
	}
}

func TestHookConfig_SetRejectsInvalidFieldsWithoutWriting(t *testing.T) {
	root, _, _ := setupCLIRepo(t)
	previous, _ := os.Getwd()
	defer os.Chdir(previous)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	before, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cmd := newHookConfigSetCommand()
	cmd.SetArgs([]string{"--fields", "coverage,unknown"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("invalid fields accepted")
	}
	after, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(before.HookSettings().Trailer.Fields, ",") != strings.Join(after.HookSettings().Trailer.Fields, ",") {
		t.Fatalf("config changed: before=%#v after=%#v", before, after)
	}
}
