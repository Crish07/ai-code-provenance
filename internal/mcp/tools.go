// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"

	"ai-prov/internal/app"
	"ai-prov/internal/git"
	"ai-prov/internal/provenance"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Input schemas mirror docs/MCP-Tool-API-Specification.md §3 and §4 verbatim.
// They are advertised via tools/list and used by the handler for manual
// validation, so the structured ErrorPayload shape is preserved on failures
// (the SDK's own validation would only surface a text error).
var (
	supportInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false
}`)

	sessionStartInputSchema = json.RawMessage(`{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": { "type": "string", "minLength": 1, "maxLength": 4096 },
    "agent": { "type": "string", "minLength": 1, "maxLength": 128 },
    "model": { "type": "string", "maxLength": 256 },
    "agent_instance_id": { "type": "string", "format": "uuid" },
    "task_key": { "type": "string", "maxLength": 256 },
    "metadata": {
      "type": "object",
      "additionalProperties": { "type": "string", "maxLength": 1024 }
    }
  },
  "additionalProperties": false
}`)

	sessionFinishInputSchema = json.RawMessage(`{
  "type": "object",
  "required": ["session_id"],
  "properties": {
    "session_id": { "type": "string", "format": "uuid" },
    "agent_instance_id": { "type": "string", "format": "uuid" },
    "summary": { "type": "string", "maxLength": 4096 }
  },
  "additionalProperties": false
}`)

	sessionStatusInputSchema = json.RawMessage(`{
  "type": "object",
  "required": ["session_id"],
  "properties": {
    "session_id": { "type": "string", "format": "uuid" }
  },
  "additionalProperties": false
}`)

	sessionRecoverInputSchema   = json.RawMessage(`{"type":"object","properties":{"agent_instance_id":{"type":"string","format":"uuid"}},"additionalProperties":false}`)
	sessionHeartbeatInputSchema = json.RawMessage(`{"type":"object","required":["session_id","agent_instance_id"],"properties":{"session_id":{"type":"string","format":"uuid"},"agent_instance_id":{"type":"string","format":"uuid"}},"additionalProperties":false}`)
	sessionAbandonInputSchema   = json.RawMessage(`{"type":"object","required":["session_id","agent_instance_id","reason"],"properties":{"session_id":{"type":"string","format":"uuid"},"agent_instance_id":{"type":"string","format":"uuid"},"reason":{"type":"string","minLength":1,"maxLength":1024}},"additionalProperties":false}`)

	verifyInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "scope": { "enum": ["staged", "worktree"], "default": "staged" },
    "strict": { "type": "boolean", "default": false }
  },
  "additionalProperties": false
}`)
)

// Success response payloads mirror the API spec schemas.
type sessionStartOutput struct {
	SessionID       string `json:"session_id"`
	State           string `json:"state"`
	StartedAt       string `json:"started_at"`
	SnapshotID      string `json:"snapshot_id"`
	TrackedFiles    int    `json:"tracked_files"`
	SkippedFiles    int    `json:"skipped_files"`
	AgentInstanceID string `json:"agent_instance_id"`
}

// SupportRepositoryURL and SupportIssuesURL are public, stable destinations
// for Agents that need to report a reproducible ai-prov problem.
const (
	SupportRepositoryURL = "https://github.com/Crish07/ai-code-provenance"
	SupportIssuesURL     = SupportRepositoryURL + "/issues/new?template=bug_report.md"
)

type supportOutput struct {
	RepositoryURL string `json:"repository_url"`
	IssuesURL     string `json:"issues_url"`
}

type fileChangeOutput struct {
	Path                string `json:"path"`
	Status              string `json:"status"`
	AddedLines          int    `json:"added_lines"`
	DeletedLines        int    `json:"deleted_lines"`
	LineProvenanceCount int    `json:"line_provenance_count,omitempty"`
}

type sessionFinishOutput struct {
	SessionID      string             `json:"session_id"`
	State          string             `json:"state"`
	FinishedAt     string             `json:"finished_at"`
	ChangedFiles   int                `json:"changed_files"`
	AIAddedLines   int                `json:"ai_added_lines"`
	AIDeletedLines int                `json:"ai_deleted_lines"`
	Changes        []fileChangeOutput `json:"changes"`
	Warnings       []string           `json:"warnings,omitempty"`
}

// sessionStatusOutput mirrors docs/MCP-Tool-API-Specification.md §5. Optional
// fields are omitted when the session is active or has not yet recorded the
// corresponding value. It exposes no source content, snapshot, diff, or
// database internals.
type sessionStatusOutput struct {
	SessionID      string `json:"session_id"`
	State          string `json:"state"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
	FailureCode    string `json:"failure_code,omitempty"`
	FailureMessage string `json:"failure_message,omitempty"`
}
type sessionCandidateOutput struct {
	SessionID       string `json:"session_id"`
	Task            string `json:"task"`
	Agent           string `json:"agent"`
	AgentInstanceID string `json:"agent_instance_id"`
	StartedAt       string `json:"started_at"`
	SnapshotID      string `json:"snapshot_id"`
	TrackedFiles    int    `json:"tracked_files"`
	SnapshotBytes   int64  `json:"snapshot_bytes"`
}
type sessionRecoverOutput struct {
	Session sessionCandidateOutput `json:"session"`
}

// verifyOutput mirrors docs/MCP-Tool-API-Specification.md §6. It is identical
// in field set and tags to cli.verifyOutput so the CLI and MCP emit byte-
// identical stats for the same repository state. Files is intentionally
// omitted: the contract marks additionalProperties:false and Files is a
// CLI-report-only extension.
type verifyOutput struct {
	Status              string   `json:"status"`
	Scope               string   `json:"scope"`
	TotalAddedLines     int      `json:"total_added_lines"`
	AIAddedLines        int      `json:"ai_added_lines"`
	UntrackedAddedLines int      `json:"untracked_added_lines"`
	Coverage            float64  `json:"coverage"`
	Sessions            []string `json:"sessions,omitempty"`
	UncoveredFiles      []string `json:"uncovered_files,omitempty"`
}

// registerTools wires provenance.session_start, provenance.session_finish and
// provenance.verify onto the SDK server. The handler performs its own
// unmarshaling and validation so it can return the structured ErrorPayload on
// every failure mode.
func registerTools(s *mcp.Server, resolve projectResolver) {
	s.AddTool(&mcp.Tool{
		Name:        "provenance.support",
		Description: "Return the public source repository and GitHub issue URL for reporting a reproducible ai-prov problem. This tool does not read the workspace.",
		InputSchema: supportInputSchema,
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		arguments := bytes.TrimSpace(req.Params.Arguments)
		if len(arguments) != 0 && !bytes.Equal(arguments, []byte("{}")) && !bytes.Equal(arguments, []byte("null")) {
			return errorResult(invalidArgument("provenance.support does not accept arguments")), nil
		}
		return successResult(supportOutput{RepositoryURL: SupportRepositoryURL, IssuesURL: SupportIssuesURL}), nil
	})

	s.AddTool(&mcp.Tool{Name: "provenance.session_recover", Description: "Recover exactly one active session after an Agent lost its session ID. Returns SESSION_RECOVERY_REQUIRED when there are zero or multiple candidates.", InputSchema: sessionRecoverInputSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		in, perr := decodeRecoverInput(req.Params.Arguments)
		if perr != nil {
			return errorResult(perr), nil
		}
		project, pending, problem := resolve(ctx, req)
		if pending != nil {
			return pending, nil
		}
		if problem != nil {
			return errorResult(problem), nil
		}
		items, err := project.svc.ActiveSessions(ctx)
		if err != nil {
			return errorResult(mapError(err)), nil
		}
		candidates := make([]sessionCandidateOutput, 0, len(items))
		for _, item := range items {
			if in.AgentInstanceID != "" && item.AgentInstanceID != in.AgentInstanceID {
				continue
			}
			candidates = append(candidates, sessionCandidateOutput{item.SessionID, item.Task, item.Agent, item.AgentInstanceID, item.StartedAt, item.SnapshotID, item.TrackedFiles, item.SnapshotBytes})
		}
		if len(candidates) != 1 {
			return errorResult(&ErrorPayload{Code: "SESSION_RECOVERY_REQUIRED", Message: "session recovery requires exactly one active session; choose an explicit session or start a new session", Details: map[string]any{"active_sessions": candidates}}), nil
		}
		return successResult(sessionRecoverOutput{Session: candidates[0]}), nil
	})

	s.AddTool(&mcp.Tool{Name: "provenance.session_heartbeat", Description: "Renew an active session lease. Only its owning agent instance may heartbeat it.", InputSchema: sessionHeartbeatInputSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, pending, problem := resolve(ctx, req)
		if pending != nil {
			return pending, nil
		}
		if problem != nil {
			return errorResult(problem), nil
		}
		in, perr := decodeHeartbeatInput(req.Params.Arguments)
		if perr != nil {
			return errorResult(perr), nil
		}
		res, err := project.svc.Heartbeat(ctx, in.SessionID, in.AgentInstanceID)
		if err != nil {
			return errorResult(mapError(err)), nil
		}
		return successResult(sessionStatusOutput{SessionID: res.SessionID, State: res.State, StartedAt: res.StartedAt, FinishedAt: res.FinishedAt, FailureCode: res.FailureCode, FailureMessage: res.FailureMessage}), nil
	})

	s.AddTool(&mcp.Tool{Name: "provenance.session_abandon", Description: "Explicitly mark an active session failed when it must not be finished.", InputSchema: sessionAbandonInputSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, pending, problem := resolve(ctx, req)
		if pending != nil {
			return pending, nil
		}
		if problem != nil {
			return errorResult(problem), nil
		}
		in, perr := decodeAbandonInput(req.Params.Arguments)
		if perr != nil {
			return errorResult(perr), nil
		}
		var res app.SessionStatusResult
		var err error
		if in.AgentInstanceID == "" {
			res, err = project.svc.Abandon(ctx, in.SessionID, in.Reason)
		} else {
			res, err = project.svc.AbandonOwned(ctx, in.SessionID, in.AgentInstanceID, in.Reason)
		}
		if err != nil {
			return errorResult(mapError(err)), nil
		}
		return successResult(sessionStatusOutput{SessionID: res.SessionID, State: res.State, StartedAt: res.StartedAt, FinishedAt: res.FinishedAt, FailureCode: res.FailureCode, FailureMessage: res.FailureMessage}), nil
	})

	s.AddTool(&mcp.Tool{
		Name:        "provenance.session_start",
		Description: "Start a provenance session and persist a baseline snapshot before any tracked source file is modified.",
		InputSchema: sessionStartInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, pending, problem := resolve(ctx, req)
		if pending != nil {
			return pending, nil
		}
		if problem != nil {
			return errorResult(problem), nil
		}
		in, perr := decodeStartInput(req.Params.Arguments)
		if perr != nil {
			return errorResult(perr), nil
		}
		res, err := project.svc.Start(ctx, app.StartRequest{Task: in.Task, Agent: in.Agent, Model: in.Model, AgentInstanceID: in.AgentInstanceID, TaskKey: in.TaskKey})
		if err != nil {
			return errorResult(mapError(err)), nil
		}
		return successResult(sessionStartOutput{
			SessionID:       res.SessionID,
			State:           res.State,
			StartedAt:       res.StartedAt,
			SnapshotID:      res.SnapshotID,
			TrackedFiles:    res.TrackedFiles,
			SkippedFiles:    res.SkippedFiles,
			AgentInstanceID: res.AgentInstanceID,
		}), nil
	})

	s.AddTool(&mcp.Tool{
		Name:        "provenance.session_finish",
		Description: "Finish a provenance session: read the workspace, compute the snapshot diff, and persist change events with line provenance in one transaction.",
		InputSchema: sessionFinishInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, pending, problem := resolve(ctx, req)
		if pending != nil {
			return pending, nil
		}
		if problem != nil {
			return errorResult(problem), nil
		}
		in, perr := decodeFinishInput(req.Params.Arguments)
		if perr != nil {
			return errorResult(perr), nil
		}
		var res app.FinishResult
		var err error
		if in.AgentInstanceID == "" {
			res, err = project.svc.Finish(ctx, in.SessionID)
		} else {
			res, err = project.svc.FinishOwned(ctx, in.SessionID, in.AgentInstanceID)
		}
		if err != nil {
			return errorResult(mapError(err)), nil
		}
		changes := make([]fileChangeOutput, 0, len(res.Changes))
		for _, c := range res.Changes {
			changes = append(changes, fileChangeOutput{
				Path:                c.Path,
				Status:              c.Status,
				AddedLines:          c.AddedLines,
				DeletedLines:        c.DeletedLines,
				LineProvenanceCount: c.LineProvenanceCount,
			})
		}
		return successResult(sessionFinishOutput{
			SessionID:      res.SessionID,
			State:          res.State,
			FinishedAt:     res.FinishedAt,
			ChangedFiles:   res.ChangedFiles,
			AIAddedLines:   res.AddedLines,
			AIDeletedLines: res.DeletedLines,
			Changes:        changes,
		}), nil
	})

	s.AddTool(&mcp.Tool{
		Name:        "provenance.session_status",
		Description: "Read a session's persisted state (active, finished, or failed) and any recorded failure code. Does not read workspace files, snapshots, diffs, or line provenance; use it to confirm whether a finish timeout left the session failed before starting a new one.",
		InputSchema: sessionStatusInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, pending, problem := resolve(ctx, req)
		if pending != nil {
			return pending, nil
		}
		if problem != nil {
			return errorResult(problem), nil
		}
		in, perr := decodeStatusInput(req.Params.Arguments)
		if perr != nil {
			return errorResult(perr), nil
		}
		res, err := project.svc.Status(ctx, in.SessionID)
		if err != nil {
			return errorResult(mapError(err)), nil
		}
		return successResult(sessionStatusOutput{
			SessionID:      res.SessionID,
			State:          res.State,
			StartedAt:      res.StartedAt,
			FinishedAt:     res.FinishedAt,
			FailureCode:    res.FailureCode,
			FailureMessage: res.FailureMessage,
		}), nil
	})

	s.AddTool(&mcp.Tool{
		Name:        "provenance.verify",
		Description: "Verify added diff lines are covered by AI provenance. Reports coverage without blocking git; strict surfaces uncovered lines as a failed status.",
		InputSchema: verifyInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, pending, problem := resolve(ctx, req)
		if pending != nil {
			return pending, nil
		}
		if problem != nil {
			return errorResult(problem), nil
		}
		in, perr := decodeVerifyInput(req.Params.Arguments)
		if perr != nil {
			return errorResult(perr), nil
		}
		res, err := project.verifier.Verify(ctx, provenance.Request{Scope: in.Scope, Strict: in.Strict})
		if err != nil {
			return errorResult(mapError(err)), nil
		}
		return successResult(verifyOutput{
			Status:              string(res.Status),
			Scope:               string(res.Scope),
			TotalAddedLines:     res.TotalAddedLines,
			AIAddedLines:        res.AIAddedLines,
			UntrackedAddedLines: res.UntrackedAddedLines,
			Coverage:            res.Coverage,
			Sessions:            res.Sessions,
			UncoveredFiles:      res.UncoveredFiles,
		}), nil
	})
}

type startInput struct {
	Task            string            `json:"task"`
	Agent           string            `json:"agent,omitempty"`
	Model           string            `json:"model,omitempty"`
	AgentInstanceID string            `json:"agent_instance_id,omitempty"`
	TaskKey         string            `json:"task_key,omitempty"`
	Meta            map[string]string `json:"metadata,omitempty"`
}

type finishInput struct {
	SessionID       string `json:"session_id"`
	AgentInstanceID string `json:"agent_instance_id"`
	Summary         string `json:"summary,omitempty"`
}

// statusInput is the decoded form of the provenance.session_status arguments.
type statusInput struct {
	SessionID string `json:"session_id"`
}

type abandonInput struct {
	SessionID       string `json:"session_id"`
	AgentInstanceID string `json:"agent_instance_id"`
	Reason          string `json:"reason"`
}

type heartbeatInput struct {
	SessionID       string `json:"session_id"`
	AgentInstanceID string `json:"agent_instance_id"`
}

type recoverInput struct {
	AgentInstanceID string `json:"agent_instance_id"`
}

// verifyInput is the decoded form of the provenance.verify arguments. Scope
// defaults to staged when omitted, mirroring the schema default.
type verifyInput struct {
	Scope  git.Scope
	Strict bool
}

// decodeStartInput unmarshals and validates the session_start arguments.
func decodeStartInput(raw json.RawMessage) (startInput, *ErrorPayload) {
	var in startInput
	if len(raw) == 0 {
		return in, invalidArgument("missing arguments")
	}
	if err := unmarshalExact(raw, &in); err != nil {
		return in, invalidArgument("%v", err)
	}
	if in.Task == "" {
		return in, invalidArgument("task is required")
	}
	if len(in.Task) > 4096 {
		return in, invalidArgument("task exceeds 4096 characters")
	}
	if in.Agent != "" && (len(in.Agent) < 1 || len(in.Agent) > 128) {
		return in, invalidArgument("agent must be 1-128 characters")
	}
	if len(in.Model) > 256 {
		return in, invalidArgument("model exceeds 256 characters")
	}
	if in.AgentInstanceID != "" && !looksLikeUUID(in.AgentInstanceID) {
		return in, invalidArgument("agent_instance_id must be a UUID v4 string")
	}
	if len(in.TaskKey) > 256 {
		return in, invalidArgument("task_key exceeds 256 characters")
	}
	for k, v := range in.Meta {
		if len(v) > 1024 {
			return in, invalidArgument("metadata.%s exceeds 1024 characters", k)
		}
	}
	return in, nil
}

// decodeFinishInput unmarshals and validates the session_finish arguments.
func decodeFinishInput(raw json.RawMessage) (finishInput, *ErrorPayload) {
	var in finishInput
	if len(raw) == 0 {
		return in, invalidArgument("missing arguments")
	}
	if err := unmarshalExact(raw, &in); err != nil {
		return in, invalidArgument("%v", err)
	}
	if in.SessionID == "" {
		return in, invalidArgument("session_id is required")
	}
	if in.AgentInstanceID != "" && !looksLikeUUID(in.AgentInstanceID) {
		return in, invalidArgument("agent_instance_id must be a UUID v4 string")
	}
	if !looksLikeUUID(in.SessionID) {
		return in, invalidArgument("session_id must be a UUID v4 string")
	}
	if len(in.Summary) > 4096 {
		return in, invalidArgument("summary exceeds 4096 characters")
	}
	return in, nil
}

// decodeStatusInput unmarshals and validates the session_status arguments. It
// mirrors decodeFinishInput's UUID check so storage receives only canonical IDs.
func decodeStatusInput(raw json.RawMessage) (statusInput, *ErrorPayload) {
	var in statusInput
	if len(raw) == 0 {
		return in, invalidArgument("missing arguments")
	}
	if err := unmarshalExact(raw, &in); err != nil {
		return in, invalidArgument("%v", err)
	}
	if in.SessionID == "" {
		return in, invalidArgument("session_id is required")
	}
	if !looksLikeUUID(in.SessionID) {
		return in, invalidArgument("session_id must be a UUID v4 string")
	}
	return in, nil
}

func decodeAbandonInput(raw json.RawMessage) (abandonInput, *ErrorPayload) {
	var in abandonInput
	if len(raw) == 0 {
		return in, invalidArgument("missing arguments")
	}
	if err := unmarshalExact(raw, &in); err != nil {
		return in, invalidArgument("%v", err)
	}
	if !looksLikeUUID(in.SessionID) {
		return in, invalidArgument("session_id must be a UUID v4 string")
	}
	if !looksLikeUUID(in.AgentInstanceID) {
		return in, invalidArgument("agent_instance_id must be a UUID v4 string")
	}
	if len(in.Reason) == 0 || len(in.Reason) > 1024 {
		return in, invalidArgument("reason must be 1-1024 characters")
	}
	return in, nil
}

func decodeHeartbeatInput(raw json.RawMessage) (heartbeatInput, *ErrorPayload) {
	var in heartbeatInput
	if len(raw) == 0 {
		return in, invalidArgument("missing arguments")
	}
	if err := unmarshalExact(raw, &in); err != nil {
		return in, invalidArgument("%v", err)
	}
	if !looksLikeUUID(in.SessionID) || !looksLikeUUID(in.AgentInstanceID) {
		return in, invalidArgument("session_id and agent_instance_id must be UUID v4 strings")
	}
	return in, nil
}

func decodeRecoverInput(raw json.RawMessage) (recoverInput, *ErrorPayload) {
	var in recoverInput
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return in, nil
	}
	if err := unmarshalExact(raw, &in); err != nil {
		return in, invalidArgument("%v", err)
	}
	if in.AgentInstanceID != "" && !looksLikeUUID(in.AgentInstanceID) {
		return in, invalidArgument("agent_instance_id must be a UUID v4 string")
	}
	return in, nil
}

// decodeVerifyInput unmarshals and validates the provenance.verify arguments.
// Both fields are optional: scope defaults to staged, strict defaults to false.
func decodeVerifyInput(raw json.RawMessage) (verifyInput, *ErrorPayload) {
	var in verifyInput
	if len(raw) > 0 {
		if err := unmarshalExact(raw, &in); err != nil {
			return in, invalidArgument("%v", err)
		}
	}
	if in.Scope == "" {
		in.Scope = git.ScopeStaged
	}
	switch in.Scope {
	case git.ScopeStaged, git.ScopeWorktree:
	default:
		return in, invalidArgument("scope must be staged or worktree")
	}
	return in, nil
}

// unmarshalExact decodes JSON while rejecting unknown fields, mirroring the
// "additionalProperties": false constraint in the contract schemas.
func unmarshalExact(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// looksLikeUUID performs a lightweight UUID v4 shape check; storage layer
// provides the authoritative existence check.
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// successResult wraps a structured payload as a successful CallToolResult.
func successResult(payload any) *mcp.CallToolResult {
	body, _ := json.Marshal(payload)
	return &mcp.CallToolResult{
		StructuredContent: payload,
		Content:           []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}
}

// errorResult wraps an ErrorPayload as a failure CallToolResult.
func errorResult(p *ErrorPayload) *mcp.CallToolResult {
	body, _ := json.Marshal(p)
	return &mcp.CallToolResult{
		IsError:           true,
		StructuredContent: p,
		Content:           []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}
}
