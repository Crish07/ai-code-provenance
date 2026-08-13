// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrUnavailable signals that git is missing or the requested scope cannot be
// read. The MCP adapter maps it to GIT_UNAVAILABLE (not retryable).
var ErrUnavailable = errors.New("git unavailable")

// Scope selects which git diff to read.
type Scope string

const (
	// ScopeStaged reads the diff between HEAD and the index.
	ScopeStaged Scope = "staged"
	// ScopeWorktree reads the diff between the index and the working tree.
	ScopeWorktree Scope = "worktree"
)

// FileDiff summarizes the added effective (non-blank) lines for a single file.
type FileDiff struct {
	Path       string
	AddedLines []string
}

// Reader runs git to extract diff data. Root must be inside a git work tree.
// It never invokes a shell: every argument is passed to git as a separate
// exec.Command parameter, matching docs/Provenance-Engine-Design.md §7.
type Reader struct {
	Root string
}

// ReadDiff returns the per-file added non-blank lines for the requested scope.
// Files with no added effective lines (deletes, binary, renames without
// content changes) are omitted from the result.
func (r Reader) ReadDiff(ctx context.Context, scope Scope) ([]FileDiff, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("%w: git binary not found: %v", ErrUnavailable, err)
	}
	args := []string{"-C", r.Root, "diff", "--no-color", "--unified=0"}
	switch scope {
	case ScopeStaged, "":
		args = append(args, "--cached")
	case ScopeWorktree:
		// worktree uses the index as the baseline; no extra flag.
	default:
		return nil, fmt.Errorf("%w: unknown scope %q", ErrUnavailable, scope)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: git %s: %v%s", ErrUnavailable, scope, err, strings.TrimSpace(errBuf.String()))
	}
	return parseDiff(out.Bytes())
}

// parseDiff extracts added effective lines from a unified git diff. It only
// looks at `+` content lines that follow a `+++ b/path` header, ignoring
// context, deletes, hunk metadata, and binary/rename markers.
func parseDiff(raw []byte) ([]FileDiff, error) {
	var out []FileDiff
	var cur *FileDiff
	flush := func() {
		if cur != nil && len(cur.AddedLines) > 0 {
			out = append(out, *cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
		case strings.HasPrefix(line, "+++ "):
			path := parseDiffPath(line[4:])
			if path == "" {
				cur = nil
			} else {
				cur = &FileDiff{Path: path}
			}
		case cur != nil && strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			content := line[1:]
			if strings.TrimSpace(content) != "" {
				cur.AddedLines = append(cur.AddedLines, content)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("parse git diff: %w", err)
	}
	flush()
	return out, nil
}

// parseDiffPath extracts the project-relative file path from a `+++ ` value.
// Returns "" for /dev/null (deleted file) so the caller can skip the file.
func parseDiffPath(v string) string {
	if v == "/dev/null" {
		return ""
	}
	if len(v) > 0 && v[0] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			v = s
		}
	}
	v = strings.TrimPrefix(v, "b/")
	return filepath.ToSlash(v)
}
