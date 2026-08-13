// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ai-prov/internal/app"
	"ai-prov/internal/cli"
	"ai-prov/internal/config"
	"ai-prov/internal/snapshot"
	"ai-prov/internal/storage"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestServer_RegistersAndCallsTools exercises the MCP integration fixture:
// discover tools, then run start → finish successfully.
func TestServer_RegistersAndCallsTools(t *testing.T) {
	root := newInitializedRoot(t)
	store := openStore(t, root)
	defer store.Close()
	svc := &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}
	srv := New(svc, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.runOver(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "ai-prov-test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if !hasTool(list.Tools, "provenance.session_start") || !hasTool(list.Tools, "provenance.session_finish") || !hasTool(list.Tools, "provenance.session_recover") || !hasTool(list.Tools, "provenance.session_heartbeat") || !hasTool(list.Tools, "provenance.session_abandon") || !hasTool(list.Tools, "provenance.support") {
		t.Fatalf("tools = %#v, want provenance tools registered", toolNames(list.Tools))
	}

	startRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "provenance.session_start",
		Arguments: map[string]any{"task": "implement T-012", "agent": "codex"},
	})
	if err != nil {
		t.Fatalf("session_start call: %v", err)
	}
	if startRes.IsError {
		t.Fatalf("session_start returned tool error: %#v", startRes.StructuredContent)
	}
	var startOut sessionStartOutput
	decodeStructured(t, startRes.StructuredContent, &startOut)
	if startOut.State != "active" || startOut.SessionID == "" || startOut.SnapshotID == "" || startOut.AgentInstanceID == "" {
		t.Fatalf("session_start output = %#v", startOut)
	}
	recoverRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "provenance.session_recover", Arguments: map[string]any{}})
	if err != nil || recoverRes.IsError {
		t.Fatalf("session_recover = %#v, err=%v", recoverRes, err)
	}
	var recovered sessionRecoverOutput
	decodeStructured(t, recoverRes.StructuredContent, &recovered)
	if recovered.Session.SessionID != startOut.SessionID {
		t.Fatalf("recovered=%#v start=%#v", recovered, startOut)
	}
	heartbeatRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "provenance.session_heartbeat", Arguments: map[string]any{"session_id": startOut.SessionID, "agent_instance_id": startOut.AgentInstanceID}})
	if err != nil || heartbeatRes.IsError {
		t.Fatalf("heartbeat=%#v err=%v", heartbeatRes, err)
	}

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	finishRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "provenance.session_finish",
		Arguments: map[string]any{"session_id": startOut.SessionID},
	})
	if err != nil {
		t.Fatalf("session_finish call: %v", err)
	}
	if finishRes.IsError {
		t.Fatalf("session_finish returned tool error: %#v", finishRes.StructuredContent)
	}
	var finishOut sessionFinishOutput
	decodeStructured(t, finishRes.StructuredContent, &finishOut)
	if finishOut.State != "finished" || finishOut.SessionID != startOut.SessionID {
		t.Fatalf("session_finish output = %#v", finishOut)
	}
}

func TestMapError_SnapshotQuotaExceeded(t *testing.T) {
	p := mapError(&snapshot.QuotaExceededError{Limit: 10, Existing: 8, Required: 3})
	if p.Code != "SNAPSHOT_QUOTA_EXCEEDED" || p.Retryable {
		t.Fatalf("payload=%#v", p)
	}
	if p.Details["limit_bytes"] != int64(10) || p.Details["existing_bytes"] != int64(8) || p.Details["required_bytes"] != int64(3) {
		t.Fatalf("details=%#v", p.Details)
	}
}

func TestServer_SupportReturnsIssueDestination(t *testing.T) {
	root := newInitializedRoot(t)
	store := openStore(t, root)
	defer store.Close()
	srv := New(&app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.runOver(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "ai-prov-test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "provenance.support", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("support = %#v, err = %v", res, err)
	}
	var out supportOutput
	decodeStructured(t, res.StructuredContent, &out)
	if out.RepositoryURL != SupportRepositoryURL || out.IssuesURL != SupportIssuesURL {
		t.Fatalf("support = %#v", out)
	}
}

func TestWorkspaceServer_DiscoversProjectFromMCPRoots(t *testing.T) {
	root := newInitializedRoot(t)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	srv := NewWorkspace(nil)
	defer srv.Cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.runOver(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "ai-prov-test"}, nil)
	client.AddRoots(&mcp.Root{URI: "file://" + root})
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()
	res := callStart(t, session, ctx, "workspace root discovery")
	if res.SessionID == "" || res.State != "active" {
		t.Fatalf("session_start = %#v", res)
	}
}

// TestServer_InvalidSchemaReturnsContractError verifies that argument
// validation failures surface as structured INVALID_ARGUMENT errors.
func TestServer_InvalidSchemaReturnsContractError(t *testing.T) {
	root := newInitializedRoot(t)
	store := openStore(t, root)
	defer store.Close()
	svc := &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}
	srv := New(svc, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.runOver(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "ai-prov-test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"missing task", map[string]any{"agent": "codex"}},
		{"empty task", map[string]any{"task": ""}},
		{"unknown field", map[string]any{"task": "t", "bogus": 1}},
		{"bad session_id", map[string]any{"session_id": "not-a-uuid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "provenance.session_start",
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("CallTool error = %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected IsError=true, got %#v", res.StructuredContent)
			}
			var p ErrorPayload
			decodeStructured(t, res.StructuredContent, &p)
			if p.Code != "INVALID_ARGUMENT" {
				t.Fatalf("code = %q, want INVALID_ARGUMENT", p.Code)
			}
		})
	}
}

// TestServer_RepeatFinishReturnsSessionNotActive verifies the state machine
// contract: a second finish on the same session is rejected.
func TestServer_RepeatFinishReturnsSessionNotActive(t *testing.T) {
	root := newInitializedRoot(t)
	store := openStore(t, root)
	defer store.Close()
	svc := &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}
	srv := New(svc, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.runOver(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "ai-prov-test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	startRes := callStart(t, session, ctx, "t")
	callFinish(t, session, ctx, startRes.SessionID)

	second, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "provenance.session_finish",
		Arguments: map[string]any{"session_id": startRes.SessionID},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	if !second.IsError {
		t.Fatalf("expected IsError=true, got %#v", second.StructuredContent)
	}
	var p ErrorPayload
	decodeStructured(t, second.StructuredContent, &p)
	if p.Code != "SESSION_NOT_ACTIVE" {
		t.Fatalf("code = %q, want SESSION_NOT_ACTIVE", p.Code)
	}
}

// TestServer_UnknownSessionReturnsSessionNotFound verifies that a finish
// against an unknown session ID maps to SESSION_NOT_FOUND.
func TestServer_UnknownSessionReturnsSessionNotFound(t *testing.T) {
	root := newInitializedRoot(t)
	store := openStore(t, root)
	defer store.Close()
	svc := &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}
	srv := New(svc, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.runOver(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "ai-prov-test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "provenance.session_finish",
		Arguments: map[string]any{"session_id": "11111111-1111-4111-8111-111111111111"},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true, got %#v", res.StructuredContent)
	}
	var p ErrorPayload
	decodeStructured(t, res.StructuredContent, &p)
	if p.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("code = %q, want SESSION_NOT_FOUND", p.Code)
	}
}

// TestBootstrap_NotInitialized verifies that the server refuses to start in a
// directory that has .git but no .ai-provenance, matching the contract.
func TestBootstrap_NotInitialized(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Bootstrap(root, nil)
	if err == nil {
		t.Fatal("Bootstrap should fail when project is not initialized")
	}
	p := mapError(err)
	if p.Code != "PROJECT_NOT_INITIALIZED" {
		t.Fatalf("code = %q, want PROJECT_NOT_INITIALIZED", p.Code)
	}
}

func TestBootstrapFromEnvironment_UsesConfiguredProjectRoot(t *testing.T) {
	root := newInitializedRoot(t)
	t.Setenv(ProjectRootEnv, root)

	srv, err := BootstrapFromEnvironment(nil)
	if err != nil {
		t.Fatalf("BootstrapFromEnvironment() error = %v", err)
	}
	defer srv.Cleanup()
	if srv.svc.Root != root {
		t.Errorf("service root = %q, want %q", srv.svc.Root, root)
	}
}

// TestMapError_StorageLocked verifies the storage lock case required by the
// acceptance criteria; reproducing a real SQLite lock in a unit test is flaky,
// so the mapping is exercised directly with the sentinel error.
func TestMapError_StorageLocked(t *testing.T) {
	p := mapError(storage.ErrLocked)
	if p.Code != "STORAGE_LOCKED" || !p.Retryable {
		t.Fatalf("got %#v, want STORAGE_LOCKED retryable=true", p)
	}
}

// TestMapError_InternalForUnknown covers the fallback path.
func TestMapError_InternalForUnknown(t *testing.T) {
	p := mapError(errors.New("boom"))
	if p.Code != "INTERNAL" || p.Retryable {
		t.Fatalf("got %#v, want INTERNAL retryable=false", p)
	}
}

// TestMapError_FinishInterruptionDetails confirms that cancellation and
// timeout errors carry the stage/counts/recommended_action fields promised in
// docs/MCP-Tool-API-Specification.md §8.
func TestMapError_FinishInterruptionDetails(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"cancelled", &app.FinishInterruptedError{Cause: app.ErrFinishCancelled, Stage: "scan_hash", Scanned: 12, Total: 100, Candidates: 3}, "FINISH_CANCELLED"},
		{"timeout", &app.FinishInterruptedError{Cause: app.ErrFinishTimeout, Stage: "diff", Scanned: 100, Total: 100, Candidates: 5}, "FINISH_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := mapError(tc.err)
			if p.Code != tc.code {
				t.Fatalf("code = %q, want %s", p.Code, tc.code)
			}
			if got := p.Details["stage"]; got != tc.err.(*app.FinishInterruptedError).Stage {
				t.Errorf("stage = %v, want %s", got, tc.err.(*app.FinishInterruptedError).Stage)
			}
			if got, ok := p.Details["recommended_action"]; !ok || got == "" {
				t.Errorf("recommended_action missing or empty: %#v", p.Details)
			}
			for _, k := range []string{"scanned_files", "total_files", "candidate_files"} {
				if _, ok := p.Details[k]; !ok {
					t.Errorf("missing detail %q: %#v", k, p.Details)
				}
			}
		})
	}
}

// TestMapError_DiffResourceLimitDetails verifies the path/bytes/lines fields
// are surfaced so Agents can guide users toward reducing the change.
func TestMapError_DiffResourceLimitDetails(t *testing.T) {
	p := mapError(&app.DiffResourceLimitError{Path: "pkg/big.go", Bytes: 9000, Lines: 200})
	if p.Code != "DIFF_RESOURCE_LIMIT" {
		t.Fatalf("code = %q, want DIFF_RESOURCE_LIMIT", p.Code)
	}
	if p.Details["path"] != "pkg/big.go" || p.Details["bytes"] != 9000 || p.Details["lines"] != 200 {
		t.Fatalf("details = %#v", p.Details)
	}
	if got, ok := p.Details["recommended_action"]; !ok || got == "" {
		t.Errorf("recommended_action missing or empty: %#v", p.Details)
	}
}

func TestMapError_DiffFailedIsRetryable(t *testing.T) {
	p := mapError(fmt.Errorf("%w: synthetic diff failure", app.ErrDiffFailed))
	if p.Code != "DIFF_FAILED" || !p.Retryable {
		t.Fatalf("error payload = %#v, want DIFF_FAILED retryable=true", p)
	}
}

// TestServer_DiffResourceLimitMarksSessionFailed verifies the resource-limit
// recovery contract end to end: finish exposes DIFF_RESOURCE_LIMIT and the
// persisted session becomes terminal rather than remaining active.
func TestServer_DiffResourceLimitMarksSessionFailed(t *testing.T) {
	root := newInitializedRoot(t)
	path := filepath.Join(root, "large.go")
	var before, after strings.Builder
	for i := 0; i < 2_100; i++ {
		before.WriteString("before\n")
		after.WriteString("after\n")
	}
	if err := os.WriteFile(path, []byte(before.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, root)
	defer store.Close()
	srv := New(&app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}, nil)
	session := dialServer(t, srv)
	defer session.Close()

	started := callStart(t, session, ctx(t), "resource limit")
	if err := os.WriteFile(path, []byte(after.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	finish, err := session.CallTool(ctx(t), &mcp.CallToolParams{
		Name:      "provenance.session_finish",
		Arguments: map[string]any{"session_id": started.SessionID},
	})
	if err != nil {
		t.Fatalf("session_finish call: %v", err)
	}
	if !finish.IsError {
		t.Fatalf("session_finish = %#v, want tool error", finish.StructuredContent)
	}
	var failure ErrorPayload
	decodeStructured(t, finish.StructuredContent, &failure)
	if failure.Code != "DIFF_RESOURCE_LIMIT" {
		t.Fatalf("finish code = %q, want DIFF_RESOURCE_LIMIT", failure.Code)
	}

	status, err := session.CallTool(ctx(t), &mcp.CallToolParams{
		Name:      "provenance.session_status",
		Arguments: map[string]any{"session_id": started.SessionID},
	})
	if err != nil || status.IsError {
		t.Fatalf("session_status = %#v, err = %v", status, err)
	}
	var out sessionStatusOutput
	decodeStructured(t, status.StructuredContent, &out)
	if out.State != "failed" || out.FailureCode != "DIFF_RESOURCE_LIMIT" {
		t.Fatalf("status = %#v, want failed/DIFF_RESOURCE_LIMIT", out)
	}
}

// TestServer_SessionStatusReportsStates covers provenance.session_status for
// active, finished, failed and unknown sessions. It confirms the tool is
// read-only and exposes no source content, diff, or line provenance.
func TestServer_SessionStatusReportsStates(t *testing.T) {
	root := newInitializedRoot(t)
	store := openStore(t, root)
	defer store.Close()
	svc := &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}
	srv := New(svc, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.runOver(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "ai-prov-test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	t.Run("active_then_finished", func(t *testing.T) {
		startOut := callStart(t, session, ctx, "active status")
		active, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "provenance.session_status",
			Arguments: map[string]any{"session_id": startOut.SessionID},
		})
		if err != nil || active.IsError {
			t.Fatalf("status active = %#v, err = %v", active, err)
		}
		var activeOut sessionStatusOutput
		decodeStructured(t, active.StructuredContent, &activeOut)
		if activeOut.SessionID != startOut.SessionID || activeOut.State != "active" || activeOut.StartedAt == "" {
			t.Fatalf("active status = %#v", activeOut)
		}
		if activeOut.FinishedAt != "" || activeOut.FailureCode != "" || activeOut.FailureMessage != "" {
			t.Fatalf("active status should omit terminal fields: %#v", activeOut)
		}

		callFinish(t, session, ctx, startOut.SessionID)
		finished, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "provenance.session_status",
			Arguments: map[string]any{"session_id": startOut.SessionID},
		})
		if err != nil || finished.IsError {
			t.Fatalf("status finished = %#v, err = %v", finished, err)
		}
		var finishedOut sessionStatusOutput
		decodeStructured(t, finished.StructuredContent, &finishedOut)
		if finishedOut.State != "finished" || finishedOut.FinishedAt == "" {
			t.Fatalf("finished status = %#v", finishedOut)
		}
		if finishedOut.FailureCode != "" || finishedOut.FailureMessage != "" {
			t.Fatalf("finished status should omit failure fields: %#v", finishedOut)
		}
	})

	t.Run("failed", func(t *testing.T) {
		startOut := callStart(t, session, ctx, "failed status")
		if err := store.FailSession(ctx, startOut.SessionID, "FINISH_TIMEOUT", "session finish timed out during scan_hash (scanned 12/20 files, 0 candidates)"); err != nil {
			t.Fatal(err)
		}
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "provenance.session_status",
			Arguments: map[string]any{"session_id": startOut.SessionID},
		})
		if err != nil || res.IsError {
			t.Fatalf("status failed = %#v, err = %v", res, err)
		}
		var out sessionStatusOutput
		decodeStructured(t, res.StructuredContent, &out)
		if out.State != "failed" || out.FailureCode != "FINISH_TIMEOUT" || out.FailureMessage == "" {
			t.Fatalf("failed status = %#v", out)
		}
		if out.FinishedAt == "" {
			t.Fatalf("failed status should expose finished_at: %#v", out)
		}
	})

	t.Run("unknown_returns_session_not_found", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "provenance.session_status",
			Arguments: map[string]any{"session_id": "11111111-1111-4111-8111-111111111111"},
		})
		if err != nil {
			t.Fatalf("CallTool error = %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected IsError=true, got %#v", res.StructuredContent)
		}
		var p ErrorPayload
		decodeStructured(t, res.StructuredContent, &p)
		if p.Code != "SESSION_NOT_FOUND" {
			t.Fatalf("code = %q, want SESSION_NOT_FOUND", p.Code)
		}
	})

	t.Run("invalid_uuid", func(t *testing.T) {
		res, _ := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "provenance.session_status",
			Arguments: map[string]any{"session_id": "not-a-uuid"},
		})
		if !res.IsError {
			t.Fatalf("expected IsError=true, got %#v", res.StructuredContent)
		}
		var p ErrorPayload
		decodeStructured(t, res.StructuredContent, &p)
		if p.Code != "INVALID_ARGUMENT" {
			t.Fatalf("code = %q, want INVALID_ARGUMENT", p.Code)
		}
	})
}

func newInitializedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".ai-provenance", "snapshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".ai-provenance", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, config.Default()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("bin/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func openStore(t *testing.T, root string) *storage.Store {
	t.Helper()
	store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func callStart(t *testing.T, session *mcp.ClientSession, ctx context.Context, task string) sessionStartOutput {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "provenance.session_start",
		Arguments: map[string]any{"task": task},
	})
	if err != nil {
		t.Fatalf("session_start call: %v", err)
	}
	if res.IsError {
		t.Fatalf("session_start returned tool error: %#v", res.StructuredContent)
	}
	var out sessionStartOutput
	decodeStructured(t, res.StructuredContent, &out)
	return out
}

func callFinish(t *testing.T, session *mcp.ClientSession, ctx context.Context, sessionID string) sessionFinishOutput {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "provenance.session_finish",
		Arguments: map[string]any{"session_id": sessionID},
	})
	if err != nil {
		t.Fatalf("session_finish call: %v", err)
	}
	if res.IsError {
		t.Fatalf("session_finish returned tool error: %#v", res.StructuredContent)
	}
	var out sessionFinishOutput
	decodeStructured(t, res.StructuredContent, &out)
	return out
}

func hasTool(tools []*mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func toolNames(tools []*mcp.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func decodeStructured(t *testing.T, raw any, dst any) {
	t.Helper()
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
}

// TestServer_VerifyHappyPath runs a session to completion, stages the change,
// and expects provenance.verify to report the AI line as covered.
func TestServer_VerifyHappyPath(t *testing.T) {
	root, _, svc := setupGitRepoWithSession(t)
	srv := New(svc, nil)
	path := filepath.Join(root, "a.go")
	writeFileT(t, path, "alpha\n")
	runGitT(t, root, "add", "a.go")
	runGitT(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	writeFileT(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	runGitT(t, root, "add", "a.go")

	session := dialServer(t, srv)
	defer session.Close()

	res, err := session.CallTool(ctx(t), &mcp.CallToolParams{
		Name:      "provenance.verify",
		Arguments: map[string]any{"scope": "staged", "strict": false},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("verify returned tool error: %#v", res.StructuredContent)
	}
	var out verifyOutput
	decodeStructured(t, res.StructuredContent, &out)
	if out.Status != "ok" || out.Scope != "staged" {
		t.Fatalf("verify output=%#v", out)
	}
	if out.TotalAddedLines != 1 || out.AIAddedLines != 1 || out.UntrackedAddedLines != 0 {
		t.Fatalf("counts=%#v", out)
	}
	if out.Coverage != 1 {
		t.Fatalf("coverage=%v want 1", out.Coverage)
	}
	if len(out.Sessions) != 1 || out.Sessions[0] != start.SessionID {
		t.Fatalf("sessions=%v want [%s]", out.Sessions, start.SessionID)
	}
}

// TestServer_VerifyGitUnavailable verifies that verify against a non-git
// directory surfaces GIT_UNAVAILABLE (not retryable).
func TestServer_VerifyGitUnavailable(t *testing.T) {
	root := newInitializedRoot(t) // no .git here
	store := openStore(t, root)
	defer store.Close()
	svc := &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: store}
	srv := New(svc, nil)
	session := dialServer(t, srv)
	defer session.Close()

	res, err := session.CallTool(ctx(t), &mcp.CallToolParams{
		Name:      "provenance.verify",
		Arguments: map[string]any{"scope": "staged"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true, got %#v", res.StructuredContent)
	}
	var p ErrorPayload
	decodeStructured(t, res.StructuredContent, &p)
	if p.Code != "GIT_UNAVAILABLE" {
		t.Fatalf("code=%q want GIT_UNAVAILABLE", p.Code)
	}
	if p.Retryable {
		t.Fatalf("GIT_UNAVAILABLE must not be retryable")
	}
}

// TestServer_VerifyInvalidScope verifies a bad scope enum is rejected as
// INVALID_ARGUMENT rather than reaching git.
func TestServer_VerifyInvalidScope(t *testing.T) {
	_, _, svc := setupGitRepoWithSession(t)
	srv := New(svc, nil)
	session := dialServer(t, srv)
	defer session.Close()

	res, err := session.CallTool(ctx(t), &mcp.CallToolParams{
		Name:      "provenance.verify",
		Arguments: map[string]any{"scope": "bogus"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true, got %#v", res.StructuredContent)
	}
	var p ErrorPayload
	decodeStructured(t, res.StructuredContent, &p)
	if p.Code != "INVALID_ARGUMENT" {
		t.Fatalf("code=%q want INVALID_ARGUMENT", p.Code)
	}
}

// TestServer_VerifyParityWithCLI asserts the MCP provenance.verify text output
// is byte-identical to cli.RunVerify --json for the same repository state.
func TestServer_VerifyParityWithCLI(t *testing.T) {
	root, _, svc := setupGitRepoWithSession(t)
	path := filepath.Join(root, "a.go")
	writeFileT(t, path, "alpha\n")
	runGitT(t, root, "add", "a.go")
	runGitT(t, root, "commit", "-m", "init")

	start, err := svc.Start(context.Background(), app.StartRequest{Task: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	writeFileT(t, path, "alpha\nbeta\n")
	if _, err := svc.Finish(context.Background(), start.SessionID); err != nil {
		t.Fatal(err)
	}
	writeFileT(t, path, "alpha\nbeta\ngamma\n")
	runGitT(t, root, "add", "a.go")

	var cliOut bytes.Buffer
	if _, err := cli.RunVerify(&cliOut, root, "staged", false, true); err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	cliJSON := strings.TrimSuffix(cliOut.String(), "\n")

	srv := New(svc, nil)
	session := dialServer(t, srv)
	defer session.Close()

	res, err := session.CallTool(ctx(t), &mcp.CallToolParams{
		Name:      "provenance.verify",
		Arguments: map[string]any{"scope": "staged", "strict": false},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("verify returned tool error: %#v", res.StructuredContent)
	}
	if len(res.Content) == 0 {
		t.Fatal("no content in verify result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] type=%T", res.Content[0])
	}
	if tc.Text != cliJSON {
		t.Fatalf("parity mismatch:\ncli: %s\nmcp: %s", cliJSON, tc.Text)
	}
}

// setupGitRepoWithSession prepares a git repo with ai-provenance config and
// storage, returning the store and an app service for the test.
func setupGitRepoWithSession(t *testing.T) (root string, store *storage.Store, svc *app.Service) {
	t.Helper()
	root = t.TempDir()
	runGitT(t, root, "init")
	runGitT(t, root, "config", "user.email", "test@example.com")
	runGitT(t, root, "config", "user.name", "test")
	runGitT(t, root, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(root, ".ai-provenance"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, config.Default()); err != nil {
		t.Fatal(err)
	}
	s, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	store = s
	svc = &app.Service{Root: root, MaxFileBytes: config.DefaultMaxFileBytes, Store: s}
	return root, store, svc
}

func dialServer(t *testing.T, srv *Server) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.runOver(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "ai-prov-test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return session
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGitT(t *testing.T, root string, args ...string) {
	t.Helper()
	var errBuf bytes.Buffer
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v in %s: %v %s", args, root, err, errBuf.String())
	}
}
