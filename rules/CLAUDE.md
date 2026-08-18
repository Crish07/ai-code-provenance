# AI Code Provenance Protocol

All tracked source-code changes MUST be recorded through ai-prov MCP. Call
provenance.session_start before editing and provenance.session_finish before
reporting completion. Finish must return finished. Any MCP failure means the
task is incomplete and must be reported.

## OpenSpec exception

Do not use provenance for OpenSpec-only process work in `openspec/changes/**` or `openspec/specs/**` (including proposal, design, tasks, change specs, and archive operations). This never covers source, tests, configuration, product documentation, or files outside those paths. For mixed work, complete OpenSpec operations before start and do not edit OpenSpec files between start and finish.

## Analysis-cache directory

`.gitnexus/` is an ai-prov internally skipped analysis-cache directory. It never enters a snapshot or finish diff, so generating or cleaning its cache does not require a provenance session. This exception applies only to that tool cache; never place source, tests, configuration, or product documentation there to bypass provenance.

## Workflow

1. Confirm ai-prov init has created .ai-provenance.
2. Generate an `agent_instance_id` UUID and start a session with task, agent set to claude, model, and that ID.
3. Persist the returned session ID and agent_instance_id in durable task state that survives context compaction, then edit with any Claude Code capability.
4. Heartbeat long tasks and finish with both IDs.
5. Optionally verify staged changes in strict mode before commit.

Only provenance.session_start, provenance.session_finish,
provenance.session_status, provenance.verify, provenance.support, provenance.session_recover, provenance.session_heartbeat, and provenance.session_abandon are valid tool names. Call
provenance.support to obtain the repository and GitHub Issue URL for a
reproducible tool problem. Call provenance.session_status to confirm whether a
finish timeout left the session failed before starting a new one. After context compaction or a lost ID, call provenance.session_recover with agent_instance_id before start; never guess a candidate after SESSION_RECOVERY_REQUIRED.

## Failure and prohibitions

Stop source edits on start or finish errors and report verbatim the error code,
`details.stage`, `details.path`/`bytes`/`lines` (for DIFF_RESOURCE_LIMIT),
`details.scanned_files`/`total_files`/`candidate_files` (for FINISH_TIMEOUT
and FINISH_CANCELLED), the ai-prov version or commit, and any Host timeout you
observed. Do not attach source content, snapshots, the SQLite database, Diff
output, token, or Git config; ai-prov never asks for them.

Run init for PROJECT_NOT_INITIALIZED, start a new session after
SESSION_BASELINE_CONFLICT or any FINISH_TIMEOUT / FINISH_CANCELLED /
DIFF_RESOURCE_LIMIT, and retry STORAGE_LOCKED later. Never retry finish on a
failed session, never run finish concurrently on the same session, never edit
before start, skip finish, claim success after failure, or submit a
client-generated diff as provenance fact.
For SNAPSHOT_QUOTA_EXCEEDED, no session was created: do not edit tracked files;
report `limit_bytes`, `existing_bytes`, and `required_bytes`, then ask the user
to run snapshot GC or increase `snapshot_max_bytes`.

Unrecorded lines are never AI code.
