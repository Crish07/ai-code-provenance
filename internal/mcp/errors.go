// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package mcp

import (
	"ai-prov/internal/app"
	"ai-prov/internal/config"
	"ai-prov/internal/git"
	"ai-prov/internal/snapshot"
	"ai-prov/internal/storage"
	"errors"
	"fmt"
)

// ErrorPayload mirrors docs/MCP-Tool-API-Specification.md §2.1. It is returned
// as MCP tool error structuredContent for every business failure.
type ErrorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	Retryable bool           `json:"retryable"`
}

// retryableCodes follows docs/MCP-Tool-API-Specification.md §8.
var retryableCodes = map[string]bool{
	"SNAPSHOT_FAILED": true,
	"DIFF_FAILED":     true,
	"STORAGE_LOCKED":  true,
}

// mapError converts a domain error into the contract ErrorPayload. Unknown
// errors become INTERNAL with retryable=false.
func mapError(err error) *ErrorPayload {
	if err == nil {
		return nil
	}
	p := &ErrorPayload{Code: "INTERNAL", Message: err.Error()}
	switch {
	case errors.Is(err, config.ErrProjectNotInitialized):
		p.Code = "PROJECT_NOT_INITIALIZED"
		p.Message = "project is not initialized; run `ai-prov init` first"
	case errors.Is(err, config.ErrProjectRootNotFound):
		p.Code = "PROJECT_ROOT_NOT_FOUND"
		p.Message = err.Error()
	case errors.Is(err, app.ErrSessionNotFound):
		p.Code = "SESSION_NOT_FOUND"
		p.Message = err.Error()
	case errors.Is(err, app.ErrSessionNotActive):
		p.Code = "SESSION_NOT_ACTIVE"
		p.Message = err.Error()
	case errors.Is(err, app.ErrSessionOwnerMismatch):
		p.Code = "SESSION_OWNER_MISMATCH"
		p.Message = "agent_instance_id does not own this session"
	case errors.Is(err, app.ErrActiveSessionLimit):
		p.Code = "SESSION_ACTIVE_LIMIT"
		p.Message = "agent instance already has the configured maximum active sessions"
	case errors.Is(err, app.ErrSessionBaselineConflict):
		p.Code = "SESSION_BASELINE_CONFLICT"
		p.Message = err.Error()
	case errors.Is(err, app.ErrSnapshotFailed):
		p.Code = "SNAPSHOT_FAILED"
		p.Message = err.Error()
	case isSnapshotQuotaExceeded(err):
		p.Code = "SNAPSHOT_QUOTA_EXCEEDED"
		p.Message = "snapshot storage quota would be exceeded; run snapshot GC or increase snapshot_max_bytes"
		p.Details = snapshotQuotaDetails(err)
	case errors.Is(err, app.ErrFinishTimeout):
		p.Code = "FINISH_TIMEOUT"
		p.Message = "session finish exceeded the MCP host deadline"
		p.Details = finishInterruptionDetails(err)
	case errors.Is(err, app.ErrFinishCancelled):
		p.Code = "FINISH_CANCELLED"
		p.Message = "session finish was cancelled before changes were committed"
		p.Details = finishInterruptionDetails(err)
	case errors.Is(err, app.ErrDiffResourceLimit):
		p.Code = "DIFF_RESOURCE_LIMIT"
		p.Message = "a changed file exceeds the session finish diff resource limit; start a new session after reducing the change"
		p.Details = diffResourceLimitDetails(err)
	case errors.Is(err, app.ErrDiffFailed):
		p.Code = "DIFF_FAILED"
		p.Message = "session finish could not compute a reliable diff"
	case errors.Is(err, storage.ErrLocked):
		p.Code = "STORAGE_LOCKED"
		p.Message = "provenance storage is locked by another process"
		p.Details = map[string]any{"hint": "retry after the active session finishes"}
	case errors.Is(err, git.ErrUnavailable):
		p.Code = "GIT_UNAVAILABLE"
		p.Message = err.Error()
	}
	if retryableCodes[p.Code] {
		p.Retryable = true
	}
	return p
}

func isSnapshotQuotaExceeded(err error) bool {
	var quota *snapshot.QuotaExceededError
	return errors.As(err, &quota)
}

func snapshotQuotaDetails(err error) map[string]any {
	var quota *snapshot.QuotaExceededError
	if !errors.As(err, &quota) {
		return nil
	}
	return map[string]any{"limit_bytes": quota.Limit, "existing_bytes": quota.Existing, "required_bytes": quota.Required, "recommended_action": "run `ai-prov snapshots gc` or increase snapshot_max_bytes"}
}

func finishInterruptionDetails(err error) map[string]any {
	var interrupted *app.FinishInterruptedError
	if !errors.As(err, &interrupted) {
		return nil
	}
	return map[string]any{
		"stage":              interrupted.Stage,
		"scanned_files":      interrupted.Scanned,
		"total_files":        interrupted.Total,
		"candidate_files":    interrupted.Candidates,
		"recommended_action": "start a new session; do not retry finish on the failed session",
	}
}

// diffResourceLimitDetails surfaces the relative path and the
// post-normalization size of the file that exceeded the diff budget. It never
// includes file content, snapshots, or diff output.
func diffResourceLimitDetails(err error) map[string]any {
	var limit *app.DiffResourceLimitError
	if !errors.As(err, &limit) {
		return map[string]any{"recommended_action": "reduce the change and start a new session"}
	}
	return map[string]any{
		"path":               limit.Path,
		"bytes":              limit.Bytes,
		"lines":              limit.Lines,
		"recommended_action": "reduce the change in the listed file and start a new session",
	}
}

// invalidArgument is a helper for schema and argument validation failures.
func invalidArgument(format string, args ...any) *ErrorPayload {
	msg := fmt.Sprintf(format, args...)
	return &ErrorPayload{Code: "INVALID_ARGUMENT", Message: msg, Details: map[string]any{"field": "arguments"}}
}

func projectRootRequired(message string) *ErrorPayload {
	return &ErrorPayload{
		Code:    "PROJECT_ROOT_REQUIRED",
		Message: message,
		Details: map[string]any{"hint": "use a project-level MCP configuration or provide exactly one MCP workspace root"},
	}
}
