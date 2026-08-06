package app

import "errors"

// Domain errors returned by SessionService. The MCP adapter maps them to the
// error codes defined in docs/MCP Tool API Specification.md §6.
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
)
