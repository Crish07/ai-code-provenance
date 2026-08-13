// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"ai-prov/internal/config"
	"ai-prov/internal/snapshot"
	"ai-prov/internal/storage"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func newSnapshotsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "snapshots", Short: "Inspect snapshot retention"}
	var olderThan time.Duration
	var jsonOut, apply bool
	gc := &cobra.Command{Use: "gc", Short: "Preview reclaimable terminal snapshots", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.Load(root)
		if err != nil {
			return err
		}
		if olderThan == 0 {
			olderThan = time.Duration(cfg.SnapshotRetentionHours) * time.Hour
		}
		store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
		if err != nil {
			return err
		}
		defer store.Close()
		cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
		sessions, err := store.TerminalSessionsBefore(context.Background(), root, cutoff)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(sessions))
		for _, session := range sessions {
			ids = append(ids, session.SnapshotID)
		}
		var report snapshot.GCReport
		if apply {
			report, err = snapshot.GCApply(root, ids)
		} else {
			report, err = snapshot.GCDryRun(root, ids)
		}
		if err != nil {
			return err
		}
		if jsonOut {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		}
		mode := "dry-run"
		if apply {
			mode = "applied"
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: sessions=%d snapshot_bytes=%d objects=%d object_bytes=%d\n", mode, len(report.SessionIDs), report.SnapshotBytes, len(report.ObjectHashes), report.ObjectBytes)
		return err
	}}
	gc.Flags().DurationVar(&olderThan, "older-than", 0, "override configured terminal snapshot retention duration")
	gc.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	gc.Flags().BoolVar(&apply, "apply", false, "delete the dry-run candidates")
	cmd.AddCommand(gc)
	return cmd
}
