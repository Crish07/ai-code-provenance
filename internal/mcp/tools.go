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

// Input schemas mirror docs/MCP Tool API Specification.md §3 and §4 verbatim.
// They are advertised via tools/list and used by the handler for manual
// validation, so the structured ErrorPayload shape is preserved on failures
// (the SDK's own validation would only surface a text error).
var (
	sessionStartInputSchema = json.RawMessage(`{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": { "type": "string", "minLength": 1, "maxLength": 4096 },
    "agent": { "type": "string", "minLength": 1, "maxLength": 128 },
    "model": { "type": "string", "maxLength": 256 },
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
    "summary": { "type": "string", "maxLength": 4096 }
  },
  "additionalProperties": false
}`)

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
	SessionID    string `json:"session_id"`
	State        string `json:"state"`
	StartedAt    string `json:"started_at"`
	SnapshotID   string `json:"snapshot_id"`
	TrackedFiles int    `json:"tracked_files"`
	SkippedFiles int    `json:"skipped_files"`
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

// verifyOutput mirrors docs/MCP Tool API Specification.md §5. It is identical
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
		res, err := project.svc.Start(ctx, app.StartRequest{Task: in.Task, Agent: in.Agent, Model: in.Model})
		if err != nil {
			return errorResult(mapError(err)), nil
		}
		return successResult(sessionStartOutput{
			SessionID:    res.SessionID,
			State:        res.State,
			StartedAt:    res.StartedAt,
			SnapshotID:   res.SnapshotID,
			TrackedFiles: res.TrackedFiles,
			SkippedFiles: res.SkippedFiles,
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
		res, err := project.svc.Finish(ctx, in.SessionID)
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
	Task  string            `json:"task"`
	Agent string            `json:"agent,omitempty"`
	Model string            `json:"model,omitempty"`
	Meta  map[string]string `json:"metadata,omitempty"`
}

type finishInput struct {
	SessionID string `json:"session_id"`
	Summary   string `json:"summary,omitempty"`
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
	if !looksLikeUUID(in.SessionID) {
		return in, invalidArgument("session_id must be a UUID v4 string")
	}
	if len(in.Summary) > 4096 {
		return in, invalidArgument("summary exceeds 4096 characters")
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
