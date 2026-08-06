# Agent Rules and MCP Configuration

These public templates require coding agents to create and finish an
ai-prov provenance session around tracked source changes.

[中文说明](README.zh.md)

## Choose a template

| Agent | Source | Destination |
| --- | --- | --- |
| Codex | AGENTS.md | project-root AGENTS.md |
| Claude Code | CLAUDE.md | project-root CLAUDE.md |
| Cursor | cursor-rules.mdc | .cursor/rules/ai-prov.mdc |
| Any compatible agent | AGENT-RULES.md | Its automatic instruction file or rules directory |

Use AGENT-RULES.md when the agent is not listed above. Copy it to the file or
directory that the agent loads automatically; do not paste it only into an
interactive prompt.

## Required protocol

1. Run ai-prov init once in the tracked project.
2. Call provenance.session_start before editing.
3. Edit through any supported agent capability.
4. Call provenance.session_finish and require finished.
5. Optionally verify staged changes before committing.

Start or finish failure means the task is incomplete. Do not bypass the
protocol. Unrecorded lines are never AI code.

## MCP configuration

Every client needs a stdio server named ai-prov with command ai-prov-mcp:

~~~json
{
  "mcpServers": {
    "ai-prov": { "command": "ai-prov-mcp" }
  }
}
~~~

Use the complete tool names: provenance.session_start,
provenance.session_finish, and provenance.verify. stdout is reserved for the
MCP protocol; diagnostics go to stderr.

## Recovery

- PROJECT_NOT_INITIALIZED: run ai-prov init.
- SESSION_BASELINE_CONFLICT: discard the current session and start a new one.
- STORAGE_LOCKED: retry later.

See the root README for installation and commands.
