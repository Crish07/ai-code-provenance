# AI Code Provenance Protocol

All tracked source-code changes MUST be recorded through ai-prov MCP.

- Before editing, call provenance.session_start successfully.
- Before reporting completion, call provenance.session_finish successfully and
  require state equal to finished.
- A failed, cancelled, or unavailable MCP call means the task is incomplete.
  Do not claim success and do not bypass finish.

## Required workflow

1. Confirm ai-prov init has created .ai-provenance.
2. Call provenance.session_start with task, agent set to codex, and model.
3. Make changes using any Codex editing capability.
4. Call provenance.session_finish with the returned session ID.
5. Before commit, optionally call provenance.verify with staged and strict.

Use the full tool names above. Short names such as session_start are invalid.

## Failure handling

Stop further source edits when start or finish fails. Report the error code and
message to the user. Run init for PROJECT_NOT_INITIALIZED; start a new session
after SESSION_BASELINE_CONFLICT; retry STORAGE_LOCKED later.

Unrecorded lines are never AI code.

## Prohibited behavior

- Do not edit tracked files before start.
- Do not claim completion before successful finish, including for an empty diff.
- Do not provide a diff, source content, or line count as MCP fact input.
- Do not stage and commit manually between start and finish.
