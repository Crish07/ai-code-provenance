# Agent Rules configuration

This directory ships in each Release as a template source. It is not automatically loaded by a coding host.

## One-time setup

1. Install the Release binaries for the current user: `ai-prov install`.
2. Run `ai-prov init` in every tracked project.
3. Configure `ai-prov-mcp` as a stdio MCP server and place the matching Rules file in the location your host actually auto-loads.
4. Restart the host and verify the tool list below.

The MCP command is the installed `ai-prov-mcp` executable; it takes no URL, API key, or source-upload configuration. For a project-specific MCP configuration, set `AI_PROV_PROJECT_ROOT` only when the host cannot provide a workspace root.

## Host examples

### Codex

```toml
[mcp_servers.ai-prov]
command = "/absolute/path/to/ai-prov-mcp"
```

Copy `AGENTS.md` to the tracked project root.

### Claude Code

Merge into project `.mcp.json`:

```json
{"mcpServers":{"ai-prov":{"command":"/absolute/path/to/ai-prov-mcp"}}}
```

Copy `CLAUDE.md` to the host's loaded instruction location.

### Cursor and other hosts

Configure the same stdio command in the host's MCP settings, then copy `AGENT-RULES.md` or the host-specific template into its verified auto-loaded rules location. Do not assume a similarly named directory is loaded.

## Verify

The host must expose all eight tools:

`provenance.session_start`, `provenance.session_heartbeat`, `provenance.session_recover`, `provenance.session_finish`, `provenance.session_abandon`, `provenance.session_status`, `provenance.verify`, and `provenance.support`.

The Agent must persist `session_id` and `agent_instance_id`, heartbeat long tasks, and finish with both IDs. After context compaction it recovers by instance ID; it never guesses a session. Read the copied Rules template for failure handling.

For command discovery, use `ai-prov --help` and `ai-prov <command> --help`.
