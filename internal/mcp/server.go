// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ai-prov/internal/app"
	"ai-prov/internal/config"
	"ai-prov/internal/git"
	"ai-prov/internal/provenance"
	"ai-prov/internal/storage"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProjectRootEnv overrides the process working directory used to find an
// initialized provenance project. MCP hosts that launch servers outside the
// workspace (for example, a global IDE configuration) should set this value to
// the absolute path of the tracked project.
const ProjectRootEnv = "AI_PROV_PROJECT_ROOT"

// Implementation identifies the ai-prov-mcp server to MCP clients.
var Implementation = &mcp.Implementation{
	Name:        "ai-prov-mcp",
	Title:       "AI Code Provenance",
	Description: "Tracks local AI code provenance for the project it serves.",
	Version:     "v0.1.0",
}

// Server owns the MCP server lifecycle and the dependencies it was bootstrapped
// with. The SDK type is kept inside this package so core packages never import
// the MCP SDK.
type Server struct {
	sdk     *mcp.Server
	svc     *app.Service
	cleanup func()
}

type projectRuntime struct {
	svc      *app.Service
	verifier provenance.Verifier
}

type projectResolver func(context.Context, *mcp.CallToolRequest) (*projectRuntime, *mcp.CallToolResult, *ErrorPayload)

// New registers provenance tools onto a new MCP server backed by svc. Pass a
// non-nil logger to surface server activity on stderr.
func New(svc *app.Service, logger *slog.Logger) *Server {
	opts := &mcp.ServerOptions{}
	if logger != nil {
		opts.Logger = logger
	}
	srv := mcp.NewServer(Implementation, opts)
	project := &projectRuntime{svc: svc, verifier: provenance.Verifier{Git: git.Reader{Root: svc.Root}, Store: svc.Store}}
	registerTools(srv, func(context.Context, *mcp.CallToolRequest) (*projectRuntime, *mcp.CallToolResult, *ErrorPayload) {
		return project, nil, nil
	})
	return &Server{sdk: srv, svc: svc}
}

// NewWorkspace creates an unbound server. It discovers the workspace for each
// tool call from the host's MCP Roots, falling back to a valid process cwd for
// project-scoped MCP configurations. AI_PROV_PROJECT_ROOT is an optional
// compatibility override for hosts that cannot provide either.
func NewWorkspace(logger *slog.Logger) *Server {
	opts := &mcp.ServerOptions{}
	if logger != nil {
		opts.Logger = logger
	}
	sdk := mcp.NewServer(Implementation, opts)
	resolver := &workspaceResolver{projects: make(map[string]*projectRuntime)}
	registerTools(sdk, resolver.resolve)
	return &Server{sdk: sdk, cleanup: resolver.close}
}

// Run serves a single MCP stdio session until ctx is cancelled or the client
// disconnects. It never writes protocol data anywhere except stdout.
func (s *Server) Run(ctx context.Context) error {
	return s.runOver(ctx, &mcp.StdioTransport{})
}

// runOver serves a session over an arbitrary transport. It is exported for
// tests; production code uses Run with the stdio transport.
func (s *Server) runOver(ctx context.Context, transport mcp.Transport) error {
	return s.sdk.Run(ctx, transport)
}

// Cleanup releases resources acquired during Bootstrap. It is safe to call
// when Bootstrap did not produce a Server.
func (s *Server) Cleanup() {
	if s != nil && s.cleanup != nil {
		s.cleanup()
	}
}

// Bootstrap resolves the project root from start, loads configuration, opens
// storage, constructs the app service, and returns a ready Server plus a
// cleanup function. Failures map to the contract error codes via mapError.
func Bootstrap(start string, logger *slog.Logger) (*Server, error) {
	root, err := config.FindProjectRoot(start)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		return nil, err
	}
	svc := &app.Service{Root: root, MaxFileBytes: cfg.MaxFileBytes, MaxSnapshotBytes: cfg.SnapshotMaxBytes, LeaseTimeout: time.Duration(cfg.LeaseTimeoutMinutes) * time.Minute, MaxActivePerAgentInstance: cfg.MaxActivePerAgentInstance, ExpiredSessionGrace: time.Duration(cfg.ExpiredSessionGraceHours) * time.Hour, AutoReclaimExpiredSessions: cfg.AutoReclaimExpiredSessions, Store: store}
	srv := New(svc, logger)
	srv.cleanup = func() { _ = store.Close() }
	return srv, nil
}

// BootstrapFromEnvironment bootstraps from AI_PROV_PROJECT_ROOT when set and
// otherwise preserves the historical behavior of using the process directory.
func BootstrapFromEnvironment(logger *slog.Logger) (*Server, error) {
	start := os.Getenv(ProjectRootEnv)
	if start == "" {
		start = "."
	}
	return Bootstrap(start, logger)
}

type workspaceResolver struct {
	mu       sync.Mutex
	projects map[string]*projectRuntime
}

func (r *workspaceResolver) resolve(ctx context.Context, req *mcp.CallToolRequest) (*projectRuntime, *mcp.CallToolResult, *ErrorPayload) {
	if root := os.Getenv(ProjectRootEnv); root != "" {
		return r.open(root)
	}
	if project, result, problem := r.open("."); project != nil {
		return project, result, problem
	}
	if response, ok := req.Params.InputResponses["workspace_root"]; ok {
		roots, ok := response.(*mcp.ListRootsResult)
		if !ok || len(roots.Roots) == 0 {
			return nil, nil, projectRootRequired("MCP host did not provide a workspace root")
		}
		if len(roots.Roots) != 1 {
			return nil, nil, projectRootRequired("multiple workspace roots are unsupported; configure one project MCP server")
		}
		root, err := fileURIPath(roots.Roots[0].URI)
		if err != nil {
			return nil, nil, projectRootRequired(err.Error())
		}
		return r.open(root)
	}
	return nil, &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{"workspace_root": &mcp.ListRootsParams{}}}, nil
}

func (r *workspaceResolver) open(start string) (*projectRuntime, *mcp.CallToolResult, *ErrorPayload) {
	root, err := config.FindProjectRoot(start)
	if err != nil {
		return nil, nil, mapError(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if project := r.projects[root]; project != nil {
		return project, nil, nil
	}
	cfg, err := config.Load(root)
	if err != nil {
		return nil, nil, mapError(err)
	}
	store, err := storage.Open(filepath.Join(root, ".ai-provenance", "provenance.db"))
	if err != nil {
		return nil, nil, mapError(err)
	}
	project := &projectRuntime{svc: &app.Service{Root: root, MaxFileBytes: cfg.MaxFileBytes, MaxSnapshotBytes: cfg.SnapshotMaxBytes, LeaseTimeout: time.Duration(cfg.LeaseTimeoutMinutes) * time.Minute, MaxActivePerAgentInstance: cfg.MaxActivePerAgentInstance, ExpiredSessionGrace: time.Duration(cfg.ExpiredSessionGraceHours) * time.Hour, AutoReclaimExpiredSessions: cfg.AutoReclaimExpiredSessions, Store: store}, verifier: provenance.Verifier{Git: git.Reader{Root: root}, Store: store}}
	r.projects[root] = project
	return project, nil, nil
}

func (r *workspaceResolver) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, project := range r.projects {
		_ = project.svc.Store.Close()
	}
}

func fileURIPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return "", fmt.Errorf("workspace root must be an absolute file URI")
	}
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", fmt.Errorf("decode workspace root: %w", err)
	}
	return path, nil
}
