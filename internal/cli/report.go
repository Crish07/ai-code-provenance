package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ai-prov/internal/config"
	"ai-prov/internal/git"
	"ai-prov/internal/provenance"
	"ai-prov/internal/storage"
	"ai-prov/internal/workspace"

	"github.com/spf13/cobra"
)

// newReportCommand builds the `ai-prov report` command: a richer view of the
// verify result plus per-line attribution and workspace-skipped items.
func newReportCommand() *cobra.Command {
	var scope string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Summarize AI vs Unknown added lines and skipped workspace files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			return RunReport(cmd.OutOrStdout(), root, scope, jsonOut)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "staged", "diff scope: staged or worktree")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON with per-line and skipped details")
	return cmd
}

// lineReportOutput is the JSON shape for a single added line.
type lineReportOutput struct {
	Content   string `json:"content"`
	Source    string `json:"source"`
	SessionID string `json:"session_id,omitempty"`
}

// fileReportOutput is the JSON shape for a single file's added-line breakdown.
type fileReportOutput struct {
	Path       string             `json:"path"`
	AddedLines []lineReportOutput `json:"added_lines"`
}

// skippedItemOutput is the JSON shape for a workspace-scan skipped file.
type skippedItemOutput struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// reportOutput extends the verify stats with per-file line reports and the
// workspace skipped items. It is CLI-only; the MCP tool does not expose report.
type reportOutput struct {
	verifyOutput
	Files   []fileReportOutput  `json:"files,omitempty"`
	Skipped []skippedItemOutput `json:"skipped,omitempty"`
}

// RunReport loads the project, runs the shared verifier, scans the workspace
// for skipped items, and writes a terminal summary or JSON report. Exported so
// the MCP parity test can invoke it without spawning cobra.
func RunReport(out io.Writer, root, scope string, jsonOut bool) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	switch git.Scope(scope) {
	case git.ScopeStaged, git.ScopeWorktree:
	default:
		return fmt.Errorf("invalid --scope %q: must be staged or worktree", scope)
	}
	store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	verifier := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := verifier.Verify(context.Background(), provenance.Request{Scope: git.Scope(scope)})
	if err != nil {
		return err
	}
	_, skipped, err := workspace.Scan(root, cfg.MaxFileBytes)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeReportJSON(out, res, skipped)
	}
	return writeReportText(out, res, skipped)
}

func writeReportJSON(out io.Writer, res provenance.Result, skipped []workspace.Skipped) error {
	files := make([]fileReportOutput, 0, len(res.Files))
	for _, fr := range res.Files {
		lines := make([]lineReportOutput, 0, len(fr.AddedLines))
		for _, lr := range fr.AddedLines {
			lines = append(lines, lineReportOutput{
				Content:   lr.Content,
				Source:    string(lr.Source),
				SessionID: lr.SessionID,
			})
		}
		files = append(files, fileReportOutput{Path: fr.Path, AddedLines: lines})
	}
	sk := make([]skippedItemOutput, 0, len(skipped))
	for _, s := range skipped {
		sk = append(sk, skippedItemOutput{Path: s.Path, Reason: s.Reason})
	}
	body, err := json.Marshal(reportOutput{
		verifyOutput: verifyOutput{
			Status:              string(res.Status),
			Scope:               string(res.Scope),
			TotalAddedLines:     res.TotalAddedLines,
			AIAddedLines:        res.AIAddedLines,
			UntrackedAddedLines: res.UntrackedAddedLines,
			Coverage:            res.Coverage,
			Sessions:            res.Sessions,
			UncoveredFiles:      res.UncoveredFiles,
		},
		Files:   files,
		Skipped: sk,
	})
	if err != nil {
		return err
	}
	_, err = out.Write(append(body, '\n'))
	return err
}

func writeReportText(out io.Writer, res provenance.Result, skipped []workspace.Skipped) error {
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
	if len(res.Files) > 0 {
		b.WriteString("\nfiles:\n")
		for _, fr := range res.Files {
			fmt.Fprintf(&b, "  %s:\n", fr.Path)
			for _, lr := range fr.AddedLines {
				tag := string(lr.Source)
				if lr.SessionID != "" {
					short := lr.SessionID
					if len(short) > 8 {
						short = short[:8]
					}
					tag = fmt.Sprintf("%s %s", tag, short)
				}
				fmt.Fprintf(&b, "    + %s  [%s]\n", lr.Content, tag)
			}
		}
	}
	switch len(skipped) {
	case 0:
		b.WriteString("\nskipped: (none)\n")
	default:
		b.WriteString("\nskipped:\n")
		for _, s := range skipped {
			fmt.Fprintf(&b, "  %s (%s)\n", s.Path, s.Reason)
		}
	}
	_, err := io.WriteString(out, b.String())
	return err
}
