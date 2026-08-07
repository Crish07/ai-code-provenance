# Agent Rules Configuration

This directory is included in every release archive. It is a template source,
not an automatically loaded rules directory. Configure the local MCP server,
then ask the coding agent that will work on the project to identify its own
automatic rules location and copy the matching rule file there.

## Responsibility boundary

**You configure once:** download the release, add the MCP server, let your
agent install one rule file into its actual automatic rules location, and run
`ai-prov init` in each new tracked project.

**The agent does per task:** starts a session before edits and finishes it
after edits. This only applies after the host confirms that it loaded the rule.

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

Before following a host-specific section, give your coding agent this request:

~~~text
Read <release-directory>/rules/README.md. Identify the rules directory that
your host automatically loads for this workspace. Install the matching ai-prov
rule there, configure ai-prov MCP, and show the host's loaded-rule list. The
release rules directory is not itself an automatically loaded location.
~~~

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

Create or merge `.trae/mcp.json` in the project root:

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/extract-directory/ai-prov-mcp-darwin-arm64",
      "env": {
        "AI_PROV_PROJECT_ROOT": "${workspaceFolder}"
      }
    }
  }
}
~~~

Then:

1. Run the matching `ai-prov ... init` command in the project root.
2. Ask Trae Agent to identify the directory shown in its loaded workspace
   rules, then copy `AGENT-RULES.md` there. Do not assume `.trae/rules/` is
   injected merely because the file exists there.
3. Restart Trae or start a new agent conversation. Confirm both the loaded
   rule list and the three `provenance.*` tools are available.

Keep `AI_PROV_PROJECT_ROOT` set to `${workspaceFolder}`; do not replace it
with a fixed project path.

### Qoder

Open **Qoder Settings → MCP → My Servers → + Add**. Add or merge the following
JSON in the configuration file that opens:

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/extract-directory/ai-prov-mcp-darwin-arm64"
    }
  }
}
~~~

Save the configuration and confirm that `ai-prov` is connected and exposes its
tools. Copy this directory's `AGENTS.md` to the tracked project root; Qoder
automatically recognizes its rules.

### Other agents

Configure an stdio MCP server named `ai-prov` using the same command, then
copy `AGENT-RULES.md` to that agent's automatic instruction location.

## 3. Verify

Restart the agent or start a new session. It must expose
`provenance.session_start`, `provenance.session_finish`, and
`provenance.verify`, plus `provenance.support`. `provenance.support` returns
the public repository and GitHub Issue URL for a reproducible tool problem.
`PROJECT_NOT_INITIALIZED` means that the CLI `init`
command has not run in the tracked project.
