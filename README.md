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

Configure MCP and install the matching agent rule from the release `rules/`
directory. See the [Rules configuration guide](rules/README.md) for the exact
Codex, Claude Code, and Cursor steps.

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
~~~

## MCP tools

| Tool | Purpose |
| --- | --- |
| provenance.session_start | Create a session and baseline snapshot. |
| provenance.session_finish | Compute a local diff and persist provenance. |
| provenance.verify | Verify staged or worktree additions. |

Run ai-prov init for an uninitialized project, create a new session after a
baseline conflict, and retry after a storage lock.

## Configure rules and MCP

Every release archive includes a `rules/` directory. Select the template for
your agent, configure the local stdio MCP server, and copy the template to its
automatic instruction location. The complete, copyable configuration is kept
in [rules/README.md](rules/README.md); read it before using ai-prov with an
agent.

### What you do once

1. Download and unzip the matching release archive.
2. Follow [rules/README.md](rules/README.md) to add `ai-prov-mcp` to your
   agent and copy that agent's rule file into the project.
3. Run the release `ai-prov` binary with `init` in each project you want to
   track.

These are manual installation and configuration steps. You must repeat step 3
only for a new project, not for every task.

### What the agent does for every coding task

The rule file makes the agent call `provenance.session_start` before editing,
edit normally, then call `provenance.session_finish` and require `finished`.
The agent may call `provenance.verify` before committing. You do not need to
create snapshots, calculate diffs, or enter line counts yourself.

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
