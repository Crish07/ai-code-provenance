// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"ai-prov/internal/install"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

func newInstallCommand() *cobra.Command {
	var dir string
	var noPath, dryRun, force bool
	cmd := &cobra.Command{Use: "install", Short: "Install ai-prov binaries for the current user", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("find running executable: %w", err)
		}
		env := install.Environment{GOOS: runtime.GOOS, Home: os.Getenv("HOME"), LocalAppData: os.Getenv("LOCALAPPDATA")}
		result, err := install.Install(install.Options{Environment: env, SourceDir: filepath.Dir(executable), InstallRoot: dir, DryRun: dryRun, Force: force, DeferReceipt: true})
		if err != nil {
			return err
		}
		if noPath {
			result.Receipt.PATH.Method = "none"
		} else if runtime.GOOS == "windows" {
			if !dryRun {
				if err := install.UpdateWindowsUserPATH(result.Layout.InstallRoot); err != nil {
					return err
				}
			}
			result.Receipt.PATH.Method = "windows_registry"
		} else {
			profile := filepath.Join(env.Home, ".zshrc")
			if shell := os.Getenv("SHELL"); filepath.Base(shell) == "bash" {
				profile = filepath.Join(env.Home, ".bashrc")
			}
			contents, readErr := os.ReadFile(profile)
			if readErr != nil && !os.IsNotExist(readErr) {
				return fmt.Errorf("read shell profile: %w", readErr)
			}
			updated, err := install.ShellFragment(string(contents), result.Layout.InstallRoot)
			if err != nil {
				return err
			}
			if !dryRun {
				if err := os.WriteFile(profile, []byte(updated), 0o644); err != nil {
					return fmt.Errorf("update shell profile: %w", err)
				}
			}
			result.Receipt.PATH.Method = "shell_fragment"
			result.Receipt.PATH.BeginMark = install.ShellBeginMark
			result.Receipt.PATH.EndMark = install.ShellEndMark
		}
		if !dryRun {
			if err := install.WriteReceipt(filepath.FromSlash(result.Layout.ReceiptPath), result.Receipt); err != nil {
				return fmt.Errorf("write installation receipt: %w", err)
			}
		}
		mode := "installed"
		if dryRun {
			mode = "dry-run"
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: ai-prov and ai-prov-mcp at %s\n", mode, result.Layout.InstallRoot)
		return err
	}}
	cmd.Flags().StringVar(&dir, "dir", "", "override the user-level binary directory")
	cmd.Flags().BoolVar(&noPath, "no-path", false, "do not modify a user PATH configuration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print without writing files")
	cmd.Flags().BoolVar(&force, "force", false, "replace differing ai-prov-managed target files")
	return cmd
}
