# ai-code-provenance

Local AI code provenance for MCP-enabled coding agents.

ai-code-provenance records declared AI sessions locally, computes actual
workspace changes, and reports provenance coverage for Git changes. It never
uploads source code, diffs, or project files.

[中文说明](README.zh.md)

## Components

- ai-prov: CLI for initialization, status, verification, and reports.
- ai-prov-mcp: stdio MCP server for session and provenance recording.
- rules: templates that require compatible agents to use MCP.

All local state is stored in .ai-provenance. Ignore that directory in the
project being tracked.

## Quick start

~~~sh
make build
cd /path/to/project
ai-prov init
~~~

Configure MCP, then ask your coding agent to read the release `rules/`
directory and install the matching rule in **its own automatically loaded
rules location**. Do not assume that copying a file to a similarly named
directory makes the host load it. See the [Rules configuration guide](rules/README.md).

Required flow: call provenance.session_start before editing; make changes;
call provenance.session_finish and require finished; optionally run
ai-prov verify --scope staged --strict before committing.

An agent must not claim success if start or finish fails. Unrecorded added
lines are uncovered, never AI code.

## Commands

~~~sh
ai-prov version
ai-prov init
ai-prov status
ai-prov debug bundle --output ai-prov-debug.zip
ai-prov verify --scope staged --strict --json
ai-prov report --scope staged --json
ai-prov hook install
ai-prov hook uninstall
~~~

## Record AI results in Git commits (optional)

MCP `session_finish` persists local provenance only; it does **not** change a
Git commit message automatically. To record the staged verification result for
every `git commit`, install the hook once from the tracked project root:

~~~sh
ai-prov hook install
~~~

The hook runs staged verification during each commit and appends this block to
the end of the commit message:

~~~text
AI-Contribution: 100%
AI-Lines: 5/5
AI-Agent: codex
AI-Confidence: 100%
AI-Provenance-ID: abcdef12
~~~

It does not alter the commit subject, and it adds no empty record when there
are no added lines. Run `ai-prov hook uninstall` to remove it. Set
`hook.write_trailer: false` in `.ai-provenance/config.yaml` to keep verification
but stop writing the commit-message block.

## MCP tools

| Tool | Purpose |
| --- | --- |
| provenance.session_start | Create a session and baseline snapshot. |
| provenance.session_finish | Compute a local diff and persist provenance. |
| provenance.verify | Verify staged or worktree additions. |
| provenance.support | Return the public repository and GitHub issue URL. |

Run ai-prov init for an uninitialized project, create a new session after a
baseline conflict, and retry after a storage lock.

## Configure rules and MCP

Every release archive includes a `rules/` directory. First configure the local
stdio MCP server. Then let the agent that will use ai-prov read
`rules/README.md`, identify its own automatic rules location, and copy the
matching template there. The release directory is a source of templates; it is
not automatically loaded by every host. The complete, copyable configuration
is kept in [rules/README.md](rules/README.md). Trae users must use its
`${workspaceFolder}` MCP configuration.

### What you do once

1. Download and unzip the matching release archive.
2. Ask your agent to read [rules/README.md](rules/README.md), add
   `ai-prov-mcp`, and copy the matching rule file into the location that its
   host actually auto-loads.
3. Run the release `ai-prov` binary with `init` in each project you want to
   track.

Use this prompt if the host's rule location is unfamiliar:

~~~text
Read <release-directory>/rules/README.md. Identify the rules directory that
your host automatically injects for this workspace. Copy the matching ai-prov
rule file there, configure the ai-prov MCP server, and show which loaded-rule
list proves the rule is active. Do not treat <release-directory>/rules/ itself
as an automatically loaded directory.
~~~

These are manual installation and configuration steps. You must repeat step 3
only for a new project, not for every task.

### What the agent does for every coding task

After the host confirms that it loaded the rule, the agent must call
`provenance.session_start` before editing, edit normally, then call
`provenance.session_finish` and require `finished`. The agent may call
`provenance.verify` before committing. You do not need to create snapshots,
calculate diffs, or enter line counts yourself.

### What ai-prov does automatically

The local MCP server creates the baseline snapshot, calculates the actual
workspace diff, records provenance in `.ai-provenance/`, and returns the
session result. All source code and provenance data remain local.

## Installation and support

Download the archive matching your operating system and CPU from
[Releases](https://github.com/Crish07/ai-code-provenance/releases). Verify it
against `SHA256SUMS.txt`, then unzip it. Use the platform-suffixed binaries by
absolute path, or rename them before adding their directory to `PATH`. On
Windows, use the full `.exe` path in the MCP client configuration.

For offline or internal distribution, mirror one verified release archive and
its matching `SHA256SUMS.txt` in the approved artifact repository. Install it
without modification; no service, network access, source upload, or project
data upload is required at runtime.

When reporting a problem, include the output of `ai-prov version`, the command
that failed, its stderr, and the operating system/architecture. Do not attach
source code, `.ai-provenance/snapshots`, diffs, tokens, credentials, or the
SQLite database. Use the issue template for a privacy-safe report.

## Development

~~~sh
make fmt
make test
make test-race
make vet
make build
~~~

make release cross-compiles six targets into dist. Release archives include
both binaries, this README, and rules.

## License

This project is licensed under the [MIT License](LICENSE).
