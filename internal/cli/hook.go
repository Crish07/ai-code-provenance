package cli

import (
	"context"
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

// errHookBlocked is returned by RunCommitMsgHook when strict mode rejects a
// commit. The cobra wrapper turns it into exit code 1, which aborts the
// commit. Diagnostics are written to stderr before returning.
var errHookBlocked = errors.New("provenance verify blocked the commit")

// newHookCommand builds the `ai-prov hook` command tree.
func newHookCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage the ai-prov Git commit-msg hook",
	}
	cmd.AddCommand(newHookInstallCommand(), newHookUninstallCommand(), newHookRunCommand())
	return cmd
}

func newHookInstallCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the commit-msg hook (backs up or refuses existing hooks)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			inst := git.Installer{Root: root}
			res, err := inst.Install(force)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), formatInstallResult(res))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "back up an existing commit-msg hook before installing")
	return cmd
}

func newHookUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the ai-prov commit-msg hook if it is managed by ai-prov",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			inst := git.Installer{Root: root}
			msg, err := inst.Uninstall()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), msg)
			return nil
		},
	}
}

func newHookRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "run",
		Short:  "Internal: invoke a hook entrypoint",
		Hidden: true,
	}
	cmd.AddCommand(newHookRunCommitMsgCommand())
	return cmd
}

func newHookRunCommitMsgCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "commit-msg <msgfile>",
		Short:         "Run verify on the staged diff and update the commit message with AI Contribution trailers",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			return RunCommitMsgHook(cmd.ErrOrStderr(), root, args[0])
		},
	}
}

// RunCommitMsgHook is the entrypoint invoked by the installed commit-msg
// hook. It loads the project config, runs the shared verifier on the staged
// diff, optionally appends an AI-Contribution trailer block to the message
// file, and aborts the commit (by returning errHookBlocked) when strict mode
// is on and verify did not reach status ok.
//
// Exported so tests can exercise the hook logic without spawning the binary.
func RunCommitMsgHook(errOut io.Writer, root, msgFile string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	settings := cfg.HookSettings()

	store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		return err
	}
	defer store.Close()

	ctx := context.Background()
	verifier := provenance.Verifier{Git: git.Reader{Root: root}, Store: store}
	res, err := verifier.Verify(ctx, provenance.Request{Scope: git.ScopeStaged, Strict: settings.Strict})
	if err != nil {
		fmt.Fprintf(errOut, "ai-prov: verify failed: %v\n", err)
		return err
	}

	if settings.WriteTrailer {
		msg, rerr := os.ReadFile(msgFile)
		if rerr != nil {
			return rerr
		}
		agents := sessionAgents(ctx, store, res.Sessions)
		trailer := RenderTrailer(res, agents)
		rewritten := RewriteCommitMessage(string(msg), trailer)
		if err := os.WriteFile(msgFile, []byte(rewritten), 0o644); err != nil {
			return err
		}
	}

	if settings.Strict && res.Status != provenance.StatusOK {
		fmt.Fprintf(errOut, "ai-prov: commit blocked: status=%s ai=%d untracked=%d total=%d coverage=%.2f\n",
			res.Status, res.AIAddedLines, res.UntrackedAddedLines, res.TotalAddedLines, res.Coverage)
		if len(res.UncoveredFiles) > 0 {
			fmt.Fprintf(errOut, "ai-prov: uncovered files: %s\n", strings.Join(res.UncoveredFiles, ", "))
		}
		return errHookBlocked
	}
	return nil
}

// formatInstallResult produces a one-line summary of an Install result.
func formatInstallResult(res git.InstallResult) string {
	switch {
	case res.AlreadyManaged:
		return fmt.Sprintf("%s: already managed by ai-prov, no change", res.Path)
	case res.BackedUp != "":
		return fmt.Sprintf("%s: installed; previous hook backed up at %s", res.Path, res.BackedUp)
	default:
		return fmt.Sprintf("%s: installed", res.Path)
	}
}
