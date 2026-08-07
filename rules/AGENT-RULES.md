# AI Code Provenance Protocol

## Mandatory protocol

All tracked source-code changes MUST be recorded through ai-prov MCP.

1. Before editing, call provenance.session_start successfully.
2. Before reporting completion, call provenance.session_finish successfully
   and require state equal to finished.
3. If either call fails, is cancelled, or returns an error, the task is
   incomplete. Stop and report the error; do not claim success.

## Workflow

1. Confirm that ai-prov init has created .ai-provenance in the project root.
2. Call provenance.session_start with a short task description, agent name,
   and model name when available.
3. Make changes using any editor, patch, workspace-edit, or shell capability.
4. Call provenance.session_finish with the returned session ID.
5. Before committing, optionally call provenance.verify with scope staged and
   strict enabled.

Only the complete tool names provenance.session_start,
provenance.session_finish, provenance.verify, and provenance.support are valid.

## Failure handling

- PROJECT_NOT_INITIALIZED: run ai-prov init.
- SESSION_BASELINE_CONFLICT: discard the current session and start a new one.
- STORAGE_LOCKED: retry later.

For a reproducible ai-prov tool problem, call `provenance.support` to obtain
the repository and GitHub Issue URL. Report the URL and sanitized reproduction
details to the user; do not submit an Issue without user authorization.

Report the returned error code and message to the user. Unrecorded lines are
never AI code.

## Prohibited behavior

- Do not modify tracked files before a successful session start.
- Do not skip finish, even when the diff is empty.
- Do not report completion before finish succeeds.
- Do not provide client-generated diffs, source text, or AI line counts as
  provenance facts.
- Do not manually stage and commit between start and finish.
