package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ai-prov/internal/app"
	"ai-prov/internal/cli"
	"ai-prov/internal/config"
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
	if !hasTool(list.Tools, "provenance.session_start") || !hasTool(list.Tools, "provenance.session_finish") {
		t.Fatalf("tools = %#v, want both provenance tools registered", toolNames(list.Tools))
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
	if startOut.State != "active" || startOut.SessionID == "" || startOut.SnapshotID == "" {
		t.Fatalf("session_start output = %#v", startOut)
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
