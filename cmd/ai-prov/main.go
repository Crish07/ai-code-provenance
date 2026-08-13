// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

// Command ai-prov is the command-line interface for local code provenance.
package main

import (
	"os"

	"ai-prov/internal/cli"
)

var (
	version = "development"
	commit  = ""
	builtAt = ""
)

func main() {
	root := cli.NewRootCommand(cli.BuildInfo{
		Version: version,
		Commit:  commit,
		BuiltAt: builtAt,
	})

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
