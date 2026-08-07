# AI Code Provenance Protocol

All tracked source-code changes MUST be recorded through ai-prov MCP. Call
provenance.session_start before editing and provenance.session_finish before
reporting completion. Finish must return finished. Any MCP failure means the
task is incomplete and must be reported.

## Workflow

1. Confirm ai-prov init has created .ai-provenance.
2. Start a session with task, agent set to claude, and model.
3. Edit with any Claude Code capability.
4. Finish with the returned session ID.
5. Optionally verify staged changes in strict mode before commit.

Only provenance.session_start, provenance.session_finish, and
provenance.verify are valid tool names. Call provenance.support to obtain the
repository and GitHub Issue URL for a reproducible tool problem.

## Failure and prohibitions

Stop source edits on start or finish errors and report the error code. Run init
for PROJECT_NOT_INITIALIZED, restart after SESSION_BASELINE_CONFLICT, and retry
STORAGE_LOCKED later. Never edit before start, skip finish, claim success after
failure, or submit a client-generated diff as provenance fact.

Unrecorded lines are never AI code.
