// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"ai-prov/internal/install"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

func newUninstallCommand() *cobra.Command {
	var dryRun, keepPath bool
	cmd := &cobra.Command{Use: "uninstall", Short: "Remove the current user's ai-prov installation", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		env := install.Environment{GOOS: runtime.GOOS, Home: os.Getenv("HOME"), LocalAppData: os.Getenv("LOCALAPPDATA")}
		layout, err := install.DefaultLayout(env)
		if err != nil {
			return err
		}
		receipt, err := install.ReadReceipt(filepath.FromSlash(layout.ReceiptPath))
		if errors.Is(err, os.ErrNotExist) {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "no ai-prov-managed installation receipt found; nothing to remove")
			return err
		}
		if err != nil {
			return err
		}
		if !keepPath && receipt.PATH.Method == "shell_fragment" {
			profile := filepath.Join(env.Home, ".zshrc")
			if shell := os.Getenv("SHELL"); filepath.Base(shell) == "bash" {
				profile = filepath.Join(env.Home, ".bashrc")
			}
			contents, readErr := os.ReadFile(profile)
			if readErr != nil && !os.IsNotExist(readErr) {
				return readErr
			}
			updated, err := install.RemoveShellFragment(string(contents))
			if err != nil {
				return err
			}
			if !dryRun && updated != string(contents) {
				if err := os.WriteFile(profile, []byte(updated), 0o644); err != nil {
					return err
				}
			}
		}
		if !keepPath && receipt.PATH.Method == "windows_registry" && !dryRun {
			if err := install.RemoveWindowsUserPATH(receipt.PATH.Entry); err != nil {
				return err
			}
		}
		if runtime.GOOS == "windows" && !dryRun {
			paths, err := install.MatchingBinaryPaths(receipt)
			if err != nil {
				return err
			}
			if err := install.ScheduleWindowsRemoval(paths); err != nil {
				return err
			}
			for _, p := range paths {
				fmt.Fprintf(cmd.OutOrStdout(), "scheduled removal after exit: %s\n", p)
			}
			if err := os.Remove(filepath.FromSlash(layout.ReceiptPath)); err != nil && !os.IsNotExist(err) {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "project items were not removed: .ai-provenance, MCP configuration, Rules, and Git hooks")
			return nil
		}
		result, err := install.Uninstall(receipt, dryRun)
		if err != nil {
			return err
		}
		if !dryRun {
			if err := os.Remove(filepath.FromSlash(layout.ReceiptPath)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		for _, p := range result.Removed {
			fmt.Fprintf(cmd.OutOrStdout(), "removed: %s\n", p)
		}
		for _, p := range result.Preserved {
			fmt.Fprintf(cmd.OutOrStdout(), "preserved (hash differs): %s\n", p)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "project items were not removed: .ai-provenance, MCP configuration, Rules, and Git hooks")
		return nil
	}}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list changes without deleting files or PATH entries")
	cmd.Flags().BoolVar(&keepPath, "keep-path", false, "do not remove ai-prov's recorded PATH entry")
	return cmd
}
