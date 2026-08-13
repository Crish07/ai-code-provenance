// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package app

import (
	"errors"
	"fmt"
)

// Domain errors returned by SessionService. The MCP adapter maps them to the
// error codes defined in docs/MCP-Tool-API-Specification.md §8.
var (
	// ErrSessionNotActive means a finish was attempted on a session that is not
	// in the active state (finished, failed, or repeated finish).
	ErrSessionNotActive = errors.New("session not active")
	// ErrSessionBaselineConflict means another finished session already changed
	// the same file, so this session's baseline can no longer be trusted.
	ErrSessionBaselineConflict = errors.New("session baseline conflict")
	// ErrSessionNotFound means the referenced session ID does not exist.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSnapshotFailed means reading or writing a snapshot failed.
	ErrSnapshotFailed = errors.New("snapshot failed")
	// ErrFinishCancelled indicates that the MCP host cancelled a finish before
	// its all-or-nothing storage transaction began.
	ErrFinishCancelled = errors.New("session finish cancelled")
	// ErrFinishTimeout indicates that the MCP host deadline expired while a
	// finish was scanning or preparing its change set.
	ErrFinishTimeout     = errors.New("session finish timed out")
	ErrDiffResourceLimit = errors.New("diff resource limit exceeded")
	// ErrDiffFailed means diff computation or its attribution preparation
	// failed for a reason other than the explicit resource limit.
	ErrDiffFailed           = errors.New("diff failed")
	ErrSessionOwnerMismatch = errors.New("session owner mismatch")
	ErrActiveSessionLimit   = errors.New("active session limit reached for agent instance")
)

// FinishInterruptedError describes a cancellation without exposing file
// content. Adapters may surface its progress fields to help users diagnose a
// host timeout safely.
type FinishInterruptedError struct {
	Cause      error
	Stage      string
	Scanned    int
	Total      int
	Candidates int
}

func (e *FinishInterruptedError) Error() string {
	return fmt.Sprintf("%v during %s (scanned %d/%d files, %d candidates)", e.Cause, e.Stage, e.Scanned, e.Total, e.Candidates)
}

func (e *FinishInterruptedError) Unwrap() error { return e.Cause }

// DiffResourceLimitError carries the relative path and the post-normalization
// size of the file whose edit script exceeded the diff budget. It does not
// retain file content; adapters may surface these fields to help Agents decide
// how to reduce the change before starting a new session.
type DiffResourceLimitError struct {
	Path  string
	Bytes int
	Lines int
}

func (e *DiffResourceLimitError) Error() string {
	return fmt.Sprintf("diff resource limit exceeded for %s (%d bytes, %d lines)", e.Path, e.Bytes, e.Lines)
}

func (e *DiffResourceLimitError) Unwrap() error { return ErrDiffResourceLimit }
