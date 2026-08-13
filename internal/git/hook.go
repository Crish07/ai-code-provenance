// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// managedMarker identifies a commit-msg hook file as owned by ai-prov. The
// installer writes it as a comment near the top of the script and Uninstall
// refuses to remove any hook that does not contain it, so a user's existing
// hook is never destroyed by an unrelated uninstall.
const managedMarker = "# managed-by: ai-prov"

// commitMsgScript is the script written to .git/hooks/commit-msg. It is POSIX
// sh, does not depend on bash, and execs ai-prov so the hook's exit status is
// ai-prov's exit status. If ai-prov is not on PATH the hook emits a diagnostic
// to stderr and exits 0 so a missing binary never blocks commits.
const commitMsgScript = `#!/bin/sh
` + managedMarker + `
# Installed by ` + "`ai-prov hook install`" + `. Run ` + "`ai-prov hook uninstall`" + ` to remove.
if ! command -v ai-prov >/dev/null 2>&1; then
  echo "ai-prov: 'ai-prov' not found on PATH; skipping provenance trailer." >&2
  echo "ai-prov: run 'ai-prov hook uninstall' to remove this hook." >&2
  exit 0
fi
exec ai-prov hook run commit-msg "$1"
`

// Installer installs and removes the ai-prov commit-msg hook in a git repo.
// All filesystem effects are confined to the hooks directory of the repo at
// Root; no other files are touched.
type Installer struct {
	Root string
}

// InstallResult describes what Install did. The zero value is invalid; Install
// always returns a non-zero result on success.
type InstallResult struct {
	// Path is the absolute path to .git/hooks/commit-msg.
	Path string
	// BackedUp is the absolute path to a backup of the previous hook, if a
	// foreign hook was displaced via --force. Empty when no backup was made.
	BackedUp string
	// AlreadyManaged is true when the existing hook already carries the
	// ai-prov marker and Install left it in place (idempotent install).
	AlreadyManaged bool
}

// Install writes the ai-prov commit-msg hook. If a hook already exists and is
// managed by ai-prov, the install is idempotent. If a foreign hook exists,
// Install returns an error unless force is true, in which case the foreign hook
// is copied to commit-msg.pre-ai-prov before the ai-prov script is written.
func (i Installer) Install(force bool) (InstallResult, error) {
	dir, err := HooksDir(i.Root)
	if err != nil {
		return InstallResult{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create hooks dir: %w", err)
	}
	path := filepath.Join(dir, "commit-msg")
	res := InstallResult{Path: path}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return res, fmt.Errorf("read existing hook: %w", err)
	}
	if len(existing) > 0 {
		if isManagedHook(existing) {
			res.AlreadyManaged = true
			return res, nil
		}
		if !force {
			return res, fmt.Errorf("commit-msg hook already exists at %s; pass --force to back it up and install", path)
		}
		backup, err := backupHook(path, existing)
		if err != nil {
			return res, err
		}
		res.BackedUp = backup
	}

	if err := os.WriteFile(path, []byte(commitMsgScript), 0o755); err != nil {
		return res, fmt.Errorf("write hook: %w", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return res, fmt.Errorf("chmod hook: %w", err)
	}
	return res, nil
}

// Uninstall removes the commit-msg hook if and only if it is managed by
// ai-prov. A foreign hook is never touched. If a backup from a forced install
// exists, it is restored in place of the removed hook.
func (i Installer) Uninstall() (string, error) {
	dir, err := HooksDir(i.Root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "commit-msg")
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("%s: no commit-msg hook present, nothing to do", path), nil
		}
		return "", fmt.Errorf("read hook: %w", err)
	}
	if !isManagedHook(existing) {
		return fmt.Sprintf("%s: not managed by ai-prov, left untouched", path), nil
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("remove hook: %w", err)
	}
	backup := path + ".pre-ai-prov"
	if _, err := os.Stat(backup); err == nil {
		if err := os.Rename(backup, path); err != nil {
			return fmt.Sprintf("removed %s; failed to restore backup %s: %v", path, backup, err), nil
		}
		return fmt.Sprintf("removed %s; restored previous hook from %s", path, backup), nil
	}
	return fmt.Sprintf("removed %s", path), nil
}

// HooksDir resolves the absolute path to the git hooks directory for the
// repository at root. It uses `git rev-parse --git-path hooks` so worktrees and
// non-standard $GIT_DIR layouts resolve correctly. It never invokes a shell.
func HooksDir(root string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("%w: git binary not found: %v", ErrUnavailable, err)
	}
	cmd := exec.Command("git", "-C", root, "rev-parse", "--git-path", "hooks")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: resolve hooks dir: %v%s", ErrUnavailable, err, strings.TrimSpace(errBuf.String()))
	}
	p := strings.TrimSpace(out.String())
	if p == "" {
		return "", fmt.Errorf("%w: empty hooks path from git", ErrUnavailable)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return filepath.Clean(p), nil
}

// isManagedHook reports whether the hook content carries the ai-prov marker.
func isManagedHook(content []byte) bool {
	return bytes.Contains(content, []byte(managedMarker))
}

// backupHook copies the existing hook content to <path>.pre-ai-prov, preserving
// its executable mode so a later Uninstall can restore it verbatim.
func backupHook(path string, content []byte) (string, error) {
	backup := path + ".pre-ai-prov"
	if err := os.WriteFile(backup, content, 0o755); err != nil {
		return "", fmt.Errorf("back up existing hook: %w", err)
	}
	if err := os.Chmod(backup, 0o755); err != nil {
		return "", fmt.Errorf("chmod backup: %w", err)
	}
	return backup, nil
}
