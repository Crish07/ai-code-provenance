# AI Code Provenance Protocol

All tracked source-code changes MUST be recorded through ai-prov MCP.

## OpenSpec exception

Do not use provenance for OpenSpec-only process work in `openspec/changes/**` or `openspec/specs/**` (including proposal, design, tasks, change specs, and archive operations). This never covers source, tests, configuration, product documentation, or files outside those paths. For mixed work, complete OpenSpec operations before start and do not edit OpenSpec files between start and finish.

## Analysis-cache directory

`.gitnexus/` is seeded into `.ai-provenance/.ai-provenanceignore` as an analysis-cache directory. Keep that default rule: while present, its cache never enters a snapshot or finish diff, so generating or cleaning it does not require a provenance session. This exception applies only to that tool cache; never place source, tests, configuration, or product documentation there to bypass provenance.

- Before editing, call provenance.session_start successfully.
- Before reporting completion, call provenance.session_finish successfully and
  require state equal to finished.
- A failed, cancelled, or unavailable MCP call means the task is incomplete.
  Do not claim success and do not bypass finish.

## Required workflow

1. Confirm ai-prov init has created .ai-provenance.
2. Generate one UUID agent_instance_id for this Agent instance, then call provenance.session_start with task, agent set to codex, model, and that ID.
3. Persist the returned session ID and agent_instance_id in durable task state that survives context compaction, then make changes using any Codex editing capability.
4. For long tasks, call provenance.session_heartbeat with both IDs; call provenance.session_finish with both values.
5. Before commit, optionally call provenance.verify with staged and strict.

Use the full tool names above. `provenance.support` returns the repository and
GitHub Issue URL for reproducible tool problems. `provenance.session_status`
returns the persisted state of a session (active, finished, or failed) without
reading source files; use it to confirm whether a timed-out finish left the
session failed. After context compaction or a lost ID, call provenance.session_recover with agent_instance_id before start; do not guess when it returns SESSION_RECOVERY_REQUIRED. Short names such as session_start are invalid.

## Failure handling

Stop further source edits when start or finish fails. Report the error code,
`details.stage`, `details.path`/`bytes`/`lines` (for DIFF_RESOURCE_LIMIT),
`details.scanned_files`/`total_files`/`candidate_files` (for FINISH_TIMEOUT
and FINISH_CANCELLED), the ai-prov version or commit, and any Host-side
timeout you observed. Do not summarize these as your own. Do not attach source
content, snapshots, the SQLite database, Diff output, token, or Git config.

Run init for PROJECT_NOT_INITIALIZED; start a new session after
SESSION_BASELINE_CONFLICT or any FINISH_TIMEOUT / FINISH_CANCELLED /
DIFF_RESOURCE_LIMIT (call `provenance.session_status` to confirm the failed
state first); retry STORAGE_LOCKED later. Never retry finish on a failed
session, and never run finish concurrently on the same session.
For SNAPSHOT_QUOTA_EXCEEDED, no session was created: do not edit tracked files;
report `limit_bytes`, `existing_bytes`, and `required_bytes`, then ask the user
to run snapshot GC or increase `snapshot_max_bytes`.

Unrecorded lines are never AI code.

## Prohibited behavior

- Do not edit tracked files before start.
- Do not claim completion before successful finish, including for an empty diff.
- Do not retry finish on a session that returned FINISH_TIMEOUT,
  FINISH_CANCELLED, or DIFF_RESOURCE_LIMIT; start a new session instead.
- Do not provide a diff, source content, or line count as MCP fact input.
- Do not stage and commit manually between start and finish.
