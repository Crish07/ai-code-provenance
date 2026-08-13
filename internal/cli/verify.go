// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ai-prov/internal/config"
	"ai-prov/internal/git"
	"ai-prov/internal/provenance"
	"ai-prov/internal/storage"

	"github.com/spf13/cobra"
)

// errVerifyNonOK is returned by the verify command so cobra/main signal a
// non-zero exit when the report surfaces uncovered lines. The full report is
// already written to stdout before this error is returned.
var errVerifyNonOK = errors.New("provenance verify found uncovered lines")

// newVerifyCommand builds the `ai-prov verify` command.
func newVerifyCommand() *cobra.Command {
	var scope string
	var strict, jsonOut bool
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify added diff lines are covered by AI provenance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			status, err := RunVerify(cmd.OutOrStdout(), root, scope, strict, jsonOut)
			if err != nil {
				return err
			}
			if status != provenance.StatusOK {
				return errVerifyNonOK
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "staged", "diff scope: staged or worktree")
	cmd.Flags().BoolVar(&strict, "strict", false, "fail when any added line is uncovered")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON matching the MCP provenance.verify output")
	return cmd
}

// verifyOutput mirrors docs/MCP-Tool-API-Specification.md §6 success response.
// CLI `verify --json` and the MCP tool use the same field set and tags so the
// two surfaces emit byte-identical stats for the same repository state.
type verifyOutput struct {
	Status              string   `json:"status"`
	Scope               string   `json:"scope"`
	TotalAddedLines     int      `json:"total_added_lines"`
	AIAddedLines        int      `json:"ai_added_lines"`
	UntrackedAddedLines int      `json:"untracked_added_lines"`
	Coverage            float64  `json:"coverage"`
	Sessions            []string `json:"sessions,omitempty"`
	UncoveredFiles      []string `json:"uncovered_files,omitempty"`
}

// RunVerify loads the project, runs the shared verifier, and writes the report
// to out. It returns the resulting status and an error only for infrastructure
// failures (config/storage/git); a non-OK status is not itself an error here.
// Exported so the MCP parity test can invoke it without spawning cobra.
func RunVerify(out io.Writer, root, scope string, strict, jsonOut bool) (provenance.Status, error) {
	if _, err := config.Load(root); err != nil {
		return "", err
	}
	switch git.Scope(scope) {
	case git.ScopeStaged, git.ScopeWorktree:
	default:
		return "", fmt.Errorf("invalid --scope %q: must be staged or worktree", scope)
	}
	store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		return "", err
	}
	defer store.Close()
	verifier := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := verifier.Verify(context.Background(), provenance.Request{Scope: git.Scope(scope), Strict: strict})
	if err != nil {
		return "", err
	}
	if jsonOut {
		return writeVerifyJSON(out, res)
	}
	return writeVerifyText(out, res)
}

func writeVerifyJSON(out io.Writer, res provenance.Result) (provenance.Status, error) {
	body, err := json.Marshal(verifyOutput{
		Status:              string(res.Status),
		Scope:               string(res.Scope),
		TotalAddedLines:     res.TotalAddedLines,
		AIAddedLines:        res.AIAddedLines,
		UntrackedAddedLines: res.UntrackedAddedLines,
		Coverage:            res.Coverage,
		Sessions:            res.Sessions,
		UncoveredFiles:      res.UncoveredFiles,
	})
	if err != nil {
		return "", err
	}
	if _, err := out.Write(append(body, '\n')); err != nil {
		return "", err
	}
	return res.Status, nil
}

func writeVerifyText(out io.Writer, res provenance.Result) (provenance.Status, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "scope:    %s\n", res.Scope)
	fmt.Fprintf(&b, "status:   %s\n", res.Status)
	fmt.Fprintf(&b, "added:    %d (ai: %d, untracked: %d)\n", res.TotalAddedLines, res.AIAddedLines, res.UntrackedAddedLines)
	fmt.Fprintf(&b, "coverage: %.2f\n", res.Coverage)
	switch len(res.Sessions) {
	case 0:
		b.WriteString("sessions: (none)\n")
	default:
		fmt.Fprintf(&b, "sessions: %s\n", strings.Join(res.Sessions, ", "))
	}
	switch len(res.UncoveredFiles) {
	case 0:
		b.WriteString("uncovered files: (none)\n")
	default:
		fmt.Fprintf(&b, "uncovered files: %s\n", strings.Join(res.UncoveredFiles, ", "))
	}
	if _, err := io.WriteString(out, b.String()); err != nil {
		return "", err
	}
	return res.Status, nil
}
