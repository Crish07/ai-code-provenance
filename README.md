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

Configure an MCP client with command ai-prov-mcp, then copy the matching
template from rules: AGENTS.md for Codex, CLAUDE.md for Claude Code, or
cursor-rules.mdc for Cursor.

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

License selection is pending.
