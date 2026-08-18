# AI Code Provenance Protocol

## Mandatory protocol

All tracked source-code changes **MUST** be recorded through ai-prov MCP. The following rules are non-skippable prerequisites and take priority over quick edits, one-line fixes, user requests for immediate changes, empty diffs, or any other convenience, except for the OpenSpec process-file exception below.

1. **Call before editing.** Before creating, modifying, deleting, renaming, or using a terminal command that affects any tracked file, successfully call `provenance.session_start`.
2. **Call before completion.** Before claiming that work is complete, sending a final response, requesting user acceptance, or suggesting a commit, successfully call `provenance.session_finish` and confirm that `state` is exactly `finished`.
3. **Failure means incomplete.** If either call fails, is cancelled, times out, or returns an error, the task is incomplete. Stop further edits and completion claims, and report the error to the user. Do not claim success, assume a call succeeded, or replace the call with a later manual explanation.

## OpenSpec process-file exception

Process documents created, updated, or archived by `openspec` are outside code-provenance scope: `openspec/changes/**` (including `proposal.md`, `design.md`, `tasks.md`, change-local `specs/**`, and `archive/**`) and `openspec/specs/**`.

- When work only edits those OpenSpec process files or runs `openspec new`, `openspec instructions`, or `openspec archive`, do **not** call `provenance.session_recover`, `provenance.session_start`, `provenance.session_finish`, or heartbeat for that work.
- This exception never applies to source code, tests, build/deployment configuration, product documentation, or any file outside the OpenSpec paths; it must not be used to bypass code provenance.
- For mixed work, finish all OpenSpec operations before start, and do not edit OpenSpec paths between start and finish. If archiving or task updates remain after finish, perform them separately under this exception.

## Analysis-cache directory

`.gitnexus/` is an ai-prov internally skipped analysis-cache directory. It never enters a snapshot or finish diff, so generating or cleaning its cache does not require a provenance session. This exception applies only to that tool cache; source, tests, configuration, and product documentation must never be placed there to bypass provenance.

## Required workflow

1. Confirm that `ai-prov init` has created `.ai-provenance/` in the project root.
2. Generate one UUID `agent_instance_id` for this Agent instance. Call `provenance.session_start` with a concise, accurate `task` and that ID; provide `agent` and `model` when available.
3. Persist the returned `session_id` **and** `agent_instance_id`. Until a valid `session_id` is obtained, do not use an editor, patch, workspace-edit, file-write, rename, delete, or any shell command that can modify files.
4. Write the returned `session_id` and `agent_instance_id` into durable task state that survives context compaction. Modify tracked code or documentation only after start succeeds; OpenSpec process files follow the exception above.
5. Before calling `provenance.session_start`, call `provenance.session_recover`; if it returns exactly one active session, reuse both IDs. Normal work must not be interrupted or extended for a heartbeat. Use `provenance.session_heartbeat` only for work exceeding 24 hours when the host can run it independently. Call `provenance.session_finish` with both values when work is complete. Finish is required even when no files changed or the diff is empty.
6. Before committing, optionally call `provenance.verify` with `scope: "staged"` and `strict: true`.

Only the complete tool names `provenance.session_start`, `provenance.session_finish`, `provenance.session_status`, `provenance.verify`, `provenance.support`, `provenance.session_recover`, `provenance.session_heartbeat`, and `provenance.session_abandon` are valid. Do not invent, abbreviate, or substitute other names.

## Failure handling

- `PROJECT_NOT_INITIALIZED`: run `ai-prov init`, then run session start again.
- `SESSION_BASELINE_CONFLICT`: abandon the current session and create a new session from the latest workspace. Do not use the conflicting session to complete attribution.
- `STORAGE_LOCKED`: wait and retry; do not continue editing or claim completion until it succeeds.
- After context compaction or a lost `session_id`: call `provenance.session_recover` with the persisted `agent_instance_id`; do not blindly start again. Recovery is possible only when that instance has exactly one active session. Do not guess a candidate ID after `SESSION_RECOVERY_REQUIRED`. A session without a heartbeat longer than the configured lease timeout becomes `failed / SESSION_LEASE_EXPIRED`; create a new session and do not finish it.
- Lease-expired snapshots are reclaimed by reachability rules after the grace period only when the project explicitly enables automatic reclamation. Do not request or assume deletion of an active session's snapshot. Preview ordinary retention cleanup with `ai-prov snapshots gc` without `--apply` first.
- `FINISH_TIMEOUT` or `FINISH_CANCELLED`: the session is `failed` and finish cannot be retried. Call `provenance.session_status` with the same `session_id` to confirm its state, then create a new session. Do not finish that failed session again.
- `DIFF_RESOURCE_LIMIT`: `details` includes `path`, `bytes`, and `lines`; reduce changes in that file, then create a new session.
- `SNAPSHOT_QUOTA_EXCEEDED`: no session was created, so do not edit tracked files. Report `limit_bytes`, `existing_bytes`, and `required_bytes` faithfully, then ask the user to run snapshot GC or raise `snapshot_max_bytes`.

For a reproducible ai-prov tool problem, call `provenance.support` to obtain the public repository and GitHub Issue URL. Report that URL and sanitized reproduction details to the user. Do not submit an Issue without user authorization.

## Finish failure reporting

When session_start or session_finish fails, immediately stop further source edits and report the following fields to the user exactly as returned by MCP:

- error `code` (for example, FINISH_TIMEOUT or DIFF_RESOURCE_LIMIT);
- `details.stage`, when present (`session_load`, `scan_hash`, `diff`, or `storage_commit`);
- `details.path`, `details.bytes`, and `details.lines` for DIFF_RESOURCE_LIMIT;
- ai-prov version or commit, from the running release or `ai-prov version`;
- an observed Host-side timeout or cancellation, when present;
- `details.scanned_files`, `details.total_files`, and `details.candidate_files` for FINISH_TIMEOUT and FINISH_CANCELLED.

Do not rewrite these fields as your own summary. Do not attach source content, snapshot files, the SQLite database, Diff output, tokens, or Git configuration when reporting; ai-prov never asks an Agent to upload them.

Faithfully report the MCP error code and message to the user. Added lines that were not correctly recorded by a session are never AI code; do not describe, calculate, or commit them as AI provenance results.

## Explicitly prohibited behavior

- Modifying any tracked file before start succeeds.
- Skipping start or finish because “the change is small,” “it was a convenient fix while reading,” “the user requested urgent handling,” “the diff is empty,” or “the tool is temporarily unavailable.”
- Ending the conversation, reporting completion, requesting acceptance, or suggesting a commit before finish succeeds.
- Retrying finish on a session that returned FINISH_TIMEOUT, FINISH_CANCELLED, or DIFF_RESOURCE_LIMIT; create a new session instead. Concurrent finish calls on the same session are strictly forbidden.
- Fabricating or manually supplying a Diff, source text, AI line count, coverage, or attribution conclusion and presenting it as provenance fact.
- Manually staging or committing between start and finish; finish must happen first.
- Continuing modifications after a tool failure, or concealing a failure as success.
