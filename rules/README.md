# Agent Rules Configuration

This directory is included in every release archive. Configure the local MCP
server first, then copy the matching rule file to the project being tracked.

## Responsibility boundary

**You configure once:** download the release, add the MCP server, copy one rule
file, and run `ai-prov init` in each new tracked project.

**The agent does per task:** starts a session before edits and finishes it
after edits. The copied rule file enforces this.

**ai-prov does automatically:** creates snapshots, computes diffs, and stores
local provenance. Do not supply snapshots, diffs, or line counts yourself.

## 1. Use the release binaries

The binaries have platform suffixes. Prefer their absolute paths, for example:

~~~text
/extract-directory/ai-prov-darwin-arm64
/extract-directory/ai-prov-mcp-darwin-arm64
~~~

Initialize each tracked project once:

~~~sh
/extract-directory/ai-prov-darwin-arm64 init
~~~

The MCP server name is `ai-prov`; its command is the `ai-prov-mcp` binary. It
uses stdio and needs no arguments, URL, API key, or project path. Do not run
it manually in a terminal.

## 2. Configure your agent

### Codex

Add this to the repository `.codex/config.toml`, or run the command below:

~~~toml
[mcp_servers.ai-prov]
command = "/extract-directory/ai-prov-mcp-darwin-arm64"
~~~

~~~sh
codex mcp add ai-prov -- /extract-directory/ai-prov-mcp-darwin-arm64
~~~

Copy `AGENTS.md` to the tracked project root as `AGENTS.md`.

### Claude Code

Create or merge the tracked project's `.mcp.json`:

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/extract-directory/ai-prov-mcp-darwin-arm64"
    }
  }
}
~~~

Alternatively run
`claude mcp add --transport stdio ai-prov -- /extract-directory/ai-prov-mcp-darwin-arm64`.
Copy `CLAUDE.md` to the tracked project root.

### Cursor

Create or merge `.cursor/mcp.json` in the tracked project:

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/extract-directory/ai-prov-mcp-darwin-arm64"
    }
  }
}
~~~

Enable `ai-prov` in Cursor MCP/Tools settings, then copy `cursor-rules.mdc` to
`.cursor/rules/ai-prov.mdc`.

### Trae

Use a **project-level** MCP configuration in the tracked project, rather than
a global/user MCP entry. The project configuration selects the MCP tools used
by that project and lets the server use the active workspace as its root.

Create or merge the project's `.mcp.json`:

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/extract-directory/ai-prov-mcp-darwin-arm64"
    }
  }
}
~~~

Run the matching `ai-prov ... init` command in that project first. In Trae's
project-level agent rules, paste `AGENT-RULES.md`.

`AI_PROV_PROJECT_ROOT` is an optional compatibility override only. Use it only
when a legacy MCP host cannot provide a project working directory or MCP
workspace root; do not hard-code one project's path in a global MCP
configuration. Modern hosts require no project-path setting: ai-prov first
uses the project configuration's working directory and otherwise requests the
host's MCP workspace root automatically.

### Other agents

Configure an stdio MCP server named `ai-prov` using the same command, then
copy `AGENT-RULES.md` to that agent's automatic instruction location.

## 3. Verify

Restart the agent or start a new session. It must expose
`provenance.session_start`, `provenance.session_finish`, and
`provenance.verify`. `PROJECT_NOT_INITIALIZED` means that the CLI `init`
command has not run in the tracked project.
