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
