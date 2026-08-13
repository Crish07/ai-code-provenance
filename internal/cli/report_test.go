// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ai-prov/internal/app"
)

// TestRunReport_TerminalListsAIAndUnknownLines stages an AI line plus a human
// line and expects the terminal report to list both with their classification.
func TestRunReport_TerminalListsAIAndUnknownLines(t *testing.T) {
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
	if err := RunReport(&out, root, "staged", false); err != nil {
		t.Fatalf("RunReport: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "beta  [AI") {
		t.Fatalf("missing AI-tagged beta line:\n%s", s)
	}
	if !strings.Contains(s, "gamma  [unknown]") {
		t.Fatalf("missing unknown-tagged gamma line:\n%s", s)
	}
	if !strings.Contains(s, "skipped:") {
		t.Fatalf("missing skipped section:\n%s", s)
	}
}

// TestRunReport_JSONIncludesFilesAndSkipped checks the JSON payload carries
// the per-line files array and the skipped workspace items.
func TestRunReport_JSONIncludesFilesAndSkipped(t *testing.T) {
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

	// Add a skipped item: a binary file (contains a NUL byte).
	writeFile(t, filepath.Join(root, "bin.dat"), "bin\x00ary")

	var out bytes.Buffer
	if err := RunReport(&out, root, "staged", true); err != nil {
		t.Fatalf("RunReport: %v", err)
	}
	var got reportOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out.String())
	}
	if got.Status != "warning" || got.TotalAddedLines != 2 || got.AIAddedLines != 1 || got.UntrackedAddedLines != 1 {
		t.Fatalf("stats=%#v", got)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "a.go" {
		t.Fatalf("files=%#v", got.Files)
	}
	lines := got.Files[0].AddedLines
	if len(lines) != 2 {
		t.Fatalf("file lines=%d want 2", len(lines))
	}
	if lines[0].Content != "beta" || lines[0].Source != "AI" || lines[0].SessionID != start.SessionID {
		t.Fatalf("line 0=%#v", lines[0])
	}
	if lines[1].Content != "gamma" || lines[1].Source != "unknown" || lines[1].SessionID != "" {
		t.Fatalf("line 1=%#v", lines[1])
	}
	// bin.dat should appear in skipped with the nonutf8_or_binary reason.
	found := false
	for _, sk := range got.Skipped {
		if sk.Path == "bin.dat" {
			found = true
			if sk.Reason != "non_utf8_or_binary" {
				t.Fatalf("bin.dat reason=%q want non_utf8_or_binary", sk.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("bin.dat not in skipped=%#v", got.Skipped)
	}
}

// TestRunReport_EmptyDiffJSONHasNoFiles confirms a clean repo produces no
// files array and an ok status.
func TestRunReport_EmptyDiffJSONHasNoFiles(t *testing.T) {
	root, _, _ := setupCLIRepo(t)
	path := filepath.Join(root, "a.go")
	writeFile(t, path, "alpha\n")
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")

	var out bytes.Buffer
	if err := RunReport(&out, root, "staged", true); err != nil {
		t.Fatalf("RunReport: %v", err)
	}
	var got reportOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out.String())
	}
	if got.Status != "ok" || got.Coverage != 1 {
		t.Fatalf("stats=%#v", got)
	}
	if len(got.Files) != 0 {
		t.Fatalf("files=%#v want empty", got.Files)
	}
}
