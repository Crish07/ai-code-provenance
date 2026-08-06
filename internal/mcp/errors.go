package mcp

import (
	"ai-prov/internal/app"
	"ai-prov/internal/config"
	"ai-prov/internal/git"
	"ai-prov/internal/storage"
	"errors"
	"fmt"
)

// ErrorPayload mirrors docs/MCP Tool API Specification.md §2.1. It is returned
// as MCP tool error structuredContent for every business failure.
type ErrorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	Retryable bool           `json:"retryable"`
}

// retryableCodes follows docs/MCP Tool API Specification.md §6.
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
	case errors.Is(err, app.ErrSessionBaselineConflict):
		p.Code = "SESSION_BASELINE_CONFLICT"
		p.Message = err.Error()
	case errors.Is(err, app.ErrSnapshotFailed):
		p.Code = "SNAPSHOT_FAILED"
		p.Message = err.Error()
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

// invalidArgument is a helper for schema and argument validation failures.
func invalidArgument(format string, args ...any) *ErrorPayload {
	msg := fmt.Sprintf(format, args...)
	return &ErrorPayload{Code: "INVALID_ARGUMENT", Message: msg, Details: map[string]any{"field": "arguments"}}
}
