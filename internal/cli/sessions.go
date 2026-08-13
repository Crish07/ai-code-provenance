// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"ai-prov/internal/app"
	"ai-prov/internal/config"
	"ai-prov/internal/storage"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newSessionsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "Inspect provenance sessions"}
	var jsonOut bool
	active := &cobra.Command{Use: "active", Short: "List active sessions without source content", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err = config.Load(root); err != nil {
			return err
		}
		store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
		if err != nil {
			return err
		}
		defer store.Close()
		items, err := (app.Service{Root: root, Store: store}).ActiveSessions(context.Background())
		if err != nil {
			return err
		}
		if jsonOut {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\tfiles=%d\tbytes=%d\n", item.SessionID, item.StartedAt, item.Agent, item.Task, item.TrackedFiles, item.SnapshotBytes); err != nil {
				return err
			}
		}
		return nil
	}}
	active.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	cmd.AddCommand(active)
	return cmd
}
