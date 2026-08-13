// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

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
	cmd.AddCommand(newHookInstallCommand(), newHookUninstallCommand(), newHookConfigCommand(), newHookRunCommand())
	return cmd
}

func newHookConfigCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "View or change ai-prov Hook trailer fields"}
	cmd.AddCommand(newHookConfigShowCommand(), newHookConfigSetCommand(), newHookConfigResetCommand())
	return cmd
}

func hookConfigRoot() (string, config.Config, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", config.Config{}, err
	}
	cfg, err := config.Load(root)
	return root, cfg, err
}

func newHookConfigShowCommand() *cobra.Command {
	return &cobra.Command{Use: "show", Short: "Show effective Hook trailer configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, cfg, err := hookConfigRoot()
		if err != nil {
			return err
		}
		settings := cfg.HookSettings()
		comments := settings.Trailer.Comments != nil && *settings.Trailer.Comments
		titleCoverage := settings.TitleCoverage != nil && *settings.TitleCoverage
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "mode: %s\ntitle coverage format: [AI:%%d%%]\nfields: %s\ncomments: %t\n字段说明:\n  coverage: AI 来源覆盖率\n  lines: 已记录 AI 新增行/全部新增行\n  agent: 贡献 session 的 Agent 名称\n  provenance-id: 贡献 session ID 的前 8 位\n", hookMode(titleCoverage), strings.Join(settings.Trailer.Fields, ","), comments)
		return err
	}}
}

func newHookConfigSetCommand() *cobra.Command {
	var fields string
	var comments bool
	cmd := &cobra.Command{Use: "set", Short: "Set Hook trailer fields and comments", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		root, cfg, err := hookConfigRoot()
		if err != nil {
			return err
		}
		values := strings.Split(fields, ",")
		for i := range values {
			values[i] = strings.TrimSpace(values[i])
		}
		if err := config.ValidateTrailerFields(values); err != nil {
			return err
		}
		if cfg.Hook == nil {
			cfg.Hook = &config.HookConfig{Strict: cfg.StrictVerify, WriteTrailer: true}
		}
		cfg.Hook.Trailer = &config.TrailerConfig{Fields: values, Comments: &comments}
		return config.Save(root, cfg)
	}}
	cmd.Flags().StringVar(&fields, "fields", "", "comma-separated: coverage,lines,agent,provenance-id")
	cmd.Flags().BoolVar(&comments, "comments", false, "write an ai-prov trailer comment marker")
	_ = cmd.MarkFlagRequired("fields")
	return cmd
}

func newHookConfigResetCommand() *cobra.Command {
	return &cobra.Command{Use: "reset", Short: "Reset Hook trailer settings to coverage and agent", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		root, cfg, err := hookConfigRoot()
		if err != nil {
			return err
		}
		yes, no := true, false
		if cfg.Hook == nil {
			cfg.Hook = &config.HookConfig{Strict: cfg.StrictVerify, WriteTrailer: true}
		}
		cfg.Hook.TitleCoverage = &yes
		cfg.Hook.Trailer = &config.TrailerConfig{Fields: []string{"lines", "agent"}, Comments: &no}
		return config.Save(root, cfg)
	}}
}

func newHookInstallCommand() *cobra.Command {
	var force, trailerOnly bool
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
			if err := saveHookInstallDefaults(root, trailerOnly); err != nil {
				// The hook was just installed but its requested mode could not be
				// persisted. Restore the prior hook (when --force made a backup)
				// or remove the new one, so installation never leaves a hook whose
				// configuration has not been committed.
				if !res.AlreadyManaged {
					_, _ = inst.Uninstall()
				}
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), formatInstallResult(res))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "back up an existing commit-msg hook before installing")
	cmd.Flags().BoolVar(&trailerOnly, "trailer-only", false, "write coverage only as a trailer; do not add [AI:<n>%] to the commit title")
	return cmd
}

func saveHookInstallDefaults(root string, trailerOnly bool) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if cfg.Hook == nil {
		cfg.Hook = &config.HookConfig{Strict: cfg.StrictVerify, WriteTrailer: true}
	}
	titleCoverage := !trailerOnly
	comments := false
	fields := []string{"lines", "agent"}
	if trailerOnly {
		fields = []string{"coverage", "lines", "agent"}
	}
	cfg.Hook.TitleCoverage = &titleCoverage
	cfg.Hook.WriteTrailer = true
	cfg.Hook.Trailer = &config.TrailerConfig{Fields: fields, Comments: &comments}
	return config.Save(root, cfg)
}

func hookMode(titleCoverage bool) string {
	if titleCoverage {
		return "title-coverage"
	}
	return "trailer-only"
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

	msg, rerr := os.ReadFile(msgFile)
	if rerr != nil {
		return rerr
	}
	trailer := ""
	if settings.WriteTrailer {
		agents := sessionAgents(ctx, store, res.Sessions)
		trailer = RenderTrailer(res, agents, settings)
	}
	rewritten := RewriteCommitMessageWithCoverage(string(msg), trailer, res, settings.TitleCoverage != nil && *settings.TitleCoverage)
	if string(msg) != rewritten {
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
