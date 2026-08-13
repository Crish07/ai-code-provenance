// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

// debugBundle is deliberately limited to runtime facts. It must never grow to
// include project content or provenance data, both of which can contain source
// code and other private material.
type debugBundle struct {
	Format      string `json:"format"`
	Version     string `json:"version"`
	Commit      string `json:"commit,omitempty"`
	BuiltAt     string `json:"built_at,omitempty"`
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	GeneratedAt string `json:"generated_at"`
}

func newDebugCommand(info BuildInfo) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Create privacy-safe diagnostics",
	}
	cmd.AddCommand(newDebugBundleCommand(info))
	return cmd
}

func newDebugBundleCommand(info BuildInfo) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Create a sanitized diagnostic bundle",
		Long:  "Create a zip containing runtime metadata only. It never includes source code, provenance data, snapshots, diffs, databases, tokens, credentials, or Git configuration.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := writeDebugBundle(output, info, time.Now().UTC())
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "path for the diagnostic zip (default: current directory)")
	return cmd
}

func writeDebugBundle(output string, info BuildInfo, now time.Time) (string, error) {
	if output == "" {
		output = filepath.Join(".", "ai-prov-debug-"+now.Format("20060102T150405Z")+".zip")
	}
	if filepath.Ext(output) != ".zip" {
		return "", fmt.Errorf("debug bundle output must end in .zip")
	}

	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create debug bundle: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	archive := zip.NewWriter(file)
	entry, err := archive.Create("diagnostics.json")
	if err != nil {
		archive.Close()
		return "", fmt.Errorf("create diagnostics entry: %w", err)
	}
	payload := debugBundle{
		Format:      "ai-prov-debug-v1",
		Version:     info.Version,
		Commit:      info.Commit,
		BuiltAt:     info.BuiltAt,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		GeneratedAt: now.Format(time.RFC3339),
	}
	if err := json.NewEncoder(entry).Encode(payload); err != nil {
		archive.Close()
		return "", fmt.Errorf("write diagnostics: %w", err)
	}
	if err := archive.Close(); err != nil {
		return "", fmt.Errorf("close debug bundle: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close debug bundle file: %w", err)
	}
	return output, nil
}
