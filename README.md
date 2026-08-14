# ai-code-provenance

Local AI code provenance for MCP-enabled coding agents. ai-prov records declared local AI sessions, computes workspace changes, and reports **AI source coverage** for added Git lines. It never uploads source code, diffs, or project files.

[Repository](https://github.com/Crish07/ai-code-provenance)

[中文说明](README.zh.md)

## Install and initialize

Download the matching Release archive and verify `SHA256SUMS.txt`. Extract it, change into the extracted directory, and run the release `ai-prov` binary once to install both release binaries for the current user:

```sh
# macOS / Linux
./ai-prov install
# Open a new terminal in the working directory, then:
ai-prov init
```

```powershell
# Windows PowerShell
.\ai-prov.exe install
# Open a new terminal in the working directory, then:
ai-prov init
```

`install` copies `ai-prov` and `ai-prov-mcp`, records their SHA-256 values, and adds only an ai-prov-owned user PATH entry. `uninstall` removes only receipt-listed files whose hash still matches; it never deletes `.ai-provenance`, MCP configuration, Rules, or Git hooks. Open a new terminal after PATH changes. Use `ai-prov install --dry-run` or `ai-prov uninstall --dry-run` after installation to preview those operations.

In each tracked project:

```sh
ai-prov init
```

All project-local state is stored in `.ai-provenance`; ignore that directory in the tracked project.

## Configure MCP and Rules

Configure `ai-prov-mcp` as a local stdio MCP server, then copy one release Rules template into the location your Agent actually auto-loads. The release `rules/` directory is a template source, not an automatic instruction location.

See [Rules configuration](rules/README.md) or [中文配置](rules/README.zh.md). It contains host configuration examples and a verification checklist.

## Agent workflow

1. Generate and persist one `agent_instance_id` UUID for the Agent instance.
2. Call `provenance.session_start` before edits; persist its `session_id` and returned `agent_instance_id` across context compaction.
3. Before creating a session, call `provenance.session_recover`; when it returns one active session, reuse both IDs rather than creating a new baseline.
4. Call `provenance.session_finish` with both IDs and require `finished`.
5. Optionally run `ai-prov verify --scope staged --strict` before committing.

After lost context, call `provenance.session_recover` with the persisted instance ID. Do not guess candidates. A session past its heartbeat lease becomes `failed / SESSION_LEASE_EXPIRED`; create a new session instead of finishing it.

AI source coverage is only the proportion of added staged/worktree lines that match completed AI provenance. It is not token usage, model cost, conversation turns, elapsed time, or human/AI mixed-authorship detection.

## Complete CLI reference

Except for `install`, `uninstall`, `version`, and `completion`, project commands must be run from a project root where `ai-prov init` has completed. Append `--help` to any command to view the exact options supported by your installed version.

### Initialization, status, and version

| Command                                       | Purpose and notes                                                                                                                                                             |
| --------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ai-prov init`                                | Initializes `.ai-provenance/`, default configuration, the SQLite database, and the snapshot directory in the current project. It is safe to run again and never uploads code. |
| `ai-prov status`                              | Prints the absolute project path and counts of `active`, `finished`, and `failed` sessions. Use it to confirm the project is usable.                                          |
| `ai-prov version`                             | Prints the CLI version, commit, and build time. Run it first when checking that Rules, MCP, and binaries are from the same version.                                           |
| `ai-prov --help` / `ai-prov <command> --help` | Lists commands or subcommands and their flags; this is the authoritative entry point for discovering capabilities actually available locally.                                 |

### Session and snapshot management

| Command                                  | Purpose and notes                                                                                                                                                                                                     |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ai-prov sessions active`                | Lists active sessions without reading or printing source content. Each line contains the session ID, start time, Agent, task, file count, and snapshot bytes.                                                         |
| `ai-prov sessions active --json`         | Serializes the active-session list as JSON for scripts; it does not include source content.                                                                                                                           |
| `ai-prov snapshots gc`                   | **Dry-run by default**: previews reclaimable sessions, objects, and bytes that exceed terminal-snapshot retention. It deletes nothing.                                                                                |
| `ai-prov snapshots gc --older-than 168h` | Temporarily overrides configured terminal-snapshot retention; `168h` means seven days. This flag does not modify the configuration file.                                                                              |
| `ai-prov snapshots gc --json`            | Serializes the GC preview or apply result as JSON.                                                                                                                                                                    |
| `ai-prov snapshots gc --apply`           | Deletes terminal snapshot/object candidates selected by the current criteria. This is destructive: first run the command without `--apply` and review the scope. It can be combined with `--older-than` and `--json`. |

Automatic reclaim policy: the first `session_start` for each project each day checks reclaimable snapshots. Snapshots for completed sessions are retained for seven days by default; snapshots for sessions that failed because their lease expired also become reclaimable after seven days. Snapshots of active sessions are never automatically deleted. The default session lease is 24 hours for overnight recovery, and normal tasks do not need heartbeats. Normal CLI GC always defaults to dry-run and can be used to preview or reclaim earlier manually.

### Coverage verification and serialized report output

`verify` produces summary statistics. On top of the same statistics, `report` labels every added line as AI or unknown and lists files skipped by the workspace scan. Both operate only on the local Git diff and local provenance data; neither uploads anything.

| Command                           | Purpose and notes                                                                                                                                                                                                  |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `ai-prov verify`                  | Checks whether added lines in the staged diff (the default `staged` scope) are covered by completed AI sessions. It prints coverage, referenced sessions, and uncovered files. Any non-`ok` result exits non-zero. |
| `ai-prov verify --scope worktree` | Verifies working-tree changes relative to Git. `--scope` accepts only `staged` or `worktree`.                                                                                                                      |
| `ai-prov verify --strict`         | Strict mode fails when any added line is uncovered; use it for CI or a pre-commit gate.                                                                                                                            |
| `ai-prov verify --json`           | Writes JSON summary fields identical to MCP `provenance.verify` to standard output.                                                                                                                                |
| `ai-prov report`                  | Prints a per-file, per-line attribution (`AI` or `unknown`) for added lines and skipped workspace items. The default scope is `staged`. This is CLI-only; MCP has no `report` tool.                                |
| `ai-prov report --scope worktree` | Produces the per-line report from working-tree changes.                                                                                                                                                            |
| `ai-prov report --json`           | Writes the complete JSON report—statistics, line text, and skipped items—to standard output. It **does not create a report file automatically**.                                                                   |

Save a report with shell redirection:

```sh
# Staged report: write JSON in the current directory.
ai-prov report --json > ai-prov-report.json

# Working-tree report: also write JSON.
ai-prov report --scope worktree --json > ai-prov-report-worktree.json

# View human-readable per-line output without saving it.
ai-prov report --scope staged
```

`report --json` has the following fields. `files[].added_lines[].content` contains the real added-line text, so a report may contain source code; review it before saving, uploading, or sharing it.

```jsonc
{
  "status": "ok", // ok or warning
  "scope": "staged", // staged or worktree
  "total_added_lines": 5, // all added effective lines
  "ai_added_lines": 5, // added lines matching AI provenance
  "untracked_added_lines": 0, // added lines not matching provenance
  "coverage": 1, // AI source coverage, from 0 to 1
  "sessions": ["<session-uuid>"], // completed sessions used in this result
  "uncovered_files": [], // paths that contain unknown added lines
  "files": [
    {
      "path": "internal/example.go", // project-relative path
      "added_lines": [
        {
          "content": "func Example() {}", // added-line text; may be source code
          "source": "AI", // AI or unknown
          "session_id": "<session-uuid>", // present only for AI lines
        },
      ],
    },
  ],
  "skipped": [
    {
      "path": "assets/logo.png", // path excluded from workspace scan
      "reason": "non_utf8_or_binary", // skip reason, such as binary or non-UTF-8
    },
  ],
}
```

### Installation and removal

| Command                                                        | Purpose and notes                                                                                                                                                                        |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `./ai-prov install` (macOS/Linux Release extraction directory) | First-install entry point. Copies the release `ai-prov` and `ai-prov-mcp` into the user-level directory and adds an ai-prov-owned PATH fragment. On Windows use `.\ai-prov.exe install`. |
| `ai-prov install --dry-run`                                    | Validates release binaries and target paths only; it does not copy files, alter PATH, or write an installation receipt.                                                                  |
| `ai-prov install --dir /absolute/path`                         | Overrides the user-level installation directory. Use only an absolute path you own.                                                                                                      |
| `ai-prov install --no-path`                                    | Installs binaries without changing the shell profile or Windows user PATH; afterwards use absolute paths or manage PATH yourself.                                                        |
| `ai-prov install --force`                                      | Replaces differing ai-prov-managed target binaries only. It does not affect project provenance data.                                                                                     |
| `ai-prov uninstall --dry-run`                                  | Previews receipt-listed binaries whose SHA-256 still matches and the ai-prov PATH fragment; it deletes nothing.                                                                          |
| `ai-prov uninstall`                                            | Removes only receipt-owned, hash-matching binaries and the ai-prov-owned PATH entry. It never removes `.ai-provenance`, MCP configuration, Rules, or Git hooks.                          |
| `ai-prov uninstall --keep-path`                                | Removes binaries but retains the recorded ai-prov PATH entry.                                                                                                                            |

### Git Hook and trailers

| Command                                                           | Purpose and notes                                                                                                                                                                                                                                |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `ai-prov hook install`                                            | Installs a `commit-msg` hook and enables title coverage mode: it adds `[AI:<n>%]` to the subject and writes `lines,agent` trailers. It refuses to overwrite a non-ai-prov hook.                                                                  |
| `ai-prov hook install --force`                                    | Backs up an existing `commit-msg` hook before installing ai-prov's hook; use only when that backup is acceptable.                                                                                                                                |
| `ai-prov hook install --trailer-only`                             | Installs the hook without changing the subject; the default trailer fields become `coverage,lines,agent`.                                                                                                                                        |
| `ai-prov hook uninstall`                                          | Removes only the hook managed by ai-prov; foreign hooks are preserved or restored from backup.                                                                                                                                                   |
| `ai-prov hook config show`                                        | Shows effective trailer fields and the comment switch, with field descriptions.                                                                                                                                                                  |
| `ai-prov hook config set --fields coverage,agent --comments=true` | Sets trailer fields and, only when explicitly requested, writes the `# ai-prov trailer` comment. The default is `false`. Valid fields are only `coverage`, `lines`, `agent`, and `provenance-id`; provide comma-separated, non-duplicate values. |
| `ai-prov hook config reset`                                       | Restores title coverage mode and the `lines,agent` trailer fields, with the marker comment disabled.                                                                                                                                             |

`hook config set --fields ...` changes only the trailing fields; it does not change title coverage mode. `lines` displays recorded AI-added lines over all added lines; `provenance-id` displays the first eight characters of a contributing session ID. ai-prov has no verifiable confidence algorithm, so it does not emit `AI-Confidence`.

### Diagnostics and shell completion

| Command                                                        | Purpose and notes                                                                                                                                                                                                |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ai-prov debug bundle`                                         | Creates a privacy-safe diagnostic ZIP in the current directory and prints its path. It includes runtime metadata only—never source code, diffs, databases, snapshots, tokens, credentials, or Git configuration. |
| `ai-prov debug bundle --output /absolute/path/diagnostics.zip` | Selects the diagnostic ZIP output path. The name must end in `.zip`, and the target must not already exist.                                                                                                      |
| `ai-prov completion bash` / `zsh` / `fish` / `powershell`      | Outputs the completion script for the specified shell. Follow the loading steps printed by `ai-prov completion <shell> --help`.                                                                                  |
| `ai-prov completion <shell> --no-descriptions`                 | Generates a completion script without command descriptions, useful when a smaller script is preferred.                                                                                                           |

## Optional Git trailers

`ai-prov hook install` installs a project `commit-msg` hook. By default it writes verified coverage in the subject and concise audit data at the end:

```text
feat: add thing [AI:100%]

AI-Lines: 5/5
AI-Agent: codex
```

Use `ai-prov hook install --trailer-only` to leave the subject unchanged and write `AI-Contribution`, `AI-Lines`, and `AI-Agent` at the end instead. `ai-prov hook config set --fields ...` controls only the ending trailer fields. The marker comment is off by default; set `--comments=true` only when you explicitly need it. `AI-Confidence` is not emitted because ai-prov has no verifiable confidence calculation. `hook.write_trailer: false` still removes stale ai-prov trailers when the hook runs and preserves unrelated Git trailers.

## MCP tools

| Tool                           | Purpose                                                         |
| ------------------------------ | --------------------------------------------------------------- |
| `provenance.session_start`     | Create a baseline session.                                      |
| `provenance.session_heartbeat` | Renew the owning instance's active-session lease.               |
| `provenance.session_recover`   | Recover exactly one active session, optionally for an instance. |
| `provenance.session_finish`    | Persist a local diff and line provenance.                       |
| `provenance.session_abandon`   | Mark a confirmed active session failed.                         |
| `provenance.session_status`    | Read persisted state without source access.                     |
| `provenance.verify`            | Verify staged/worktree added-line coverage.                     |
| `provenance.support`           | Return repository and issue-report URL.                         |

## Development

```sh
gofmt -w <changed-go-files>
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/ai-prov ./cmd/ai-prov-mcp
```

## License

[MIT](LICENSE)
