// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"ai-prov/internal/config"
	"ai-prov/internal/storage"
	"ai-prov/internal/workspace"

	"github.com/spf13/cobra"
)

const legacyDefaultIgnoreRules = "# ai-prov workspace ignore rules (Git-style subset)\n"

// BuildInfo identifies the binary that is running.
type BuildInfo struct {
	Version string
	Commit  string
	BuiltAt string
}

// NewRootCommand creates the ai-prov CLI command tree.
func NewRootCommand(info BuildInfo) *cobra.Command {
	root := &cobra.Command{
		Use:          "ai-prov",
		Short:        "Track local AI code provenance",
		SilenceUsage: true,
	}

	root.AddCommand(newVersionCommand(info), newSessionsCommand(), newSnapshotsCommand())
	root.AddCommand(newInitCommand(), newStatusCommand(), newVerifyCommand(), newReportCommand(), newHookCommand(), newInstallCommand(), newUninstallCommand(), newDebugCommand(info))
	return root
}

func newInitCommand() *cobra.Command {
	return &cobra.Command{Use: "init", Short: "Initialize local provenance storage", RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		dir := filepath.Join(root, ".ai-provenance")
		if err = os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755); err != nil {
			return err
		}
		ignorePath := filepath.Join(dir, ".ai-provenanceignore")
		if contents, readErr := os.ReadFile(ignorePath); errors.Is(readErr, os.ErrNotExist) {
			if err = os.WriteFile(ignorePath, []byte(workspace.DefaultIgnoreRules), 0o644); err != nil {
				return err
			}
		} else if readErr != nil {
			return readErr
		} else if string(contents) == legacyDefaultIgnoreRules {
			if err = os.WriteFile(ignorePath, []byte(workspace.DefaultIgnoreRules), 0o644); err != nil {
				return err
			}
		}
		if _, err = config.Load(root); errors.Is(err, config.ErrProjectNotInitialized) {
			if err = config.Save(root, config.Default()); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		db, err := storage.Open(filepath.Join(dir, "provenance.db"))
		if err != nil {
			return err
		}
		return db.Close()
	}}
}
func newStatusCommand() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show provenance project status", RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err = config.Load(root); err != nil {
			return err
		}
		db, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		a, f, failed, err := db.SessionCounts(context.Background())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "project: %s\nsessions: active=%d finished=%d failed=%d\n", root, a, f, failed)
		return err
	}}
}

func newVersionCommand(info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "ai-prov %s\ncommit: %s\nbuilt at: %s\n", info.Version, info.Commit, info.BuiltAt)
			return err
		},
	}
}
