package mcp

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

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

// New registers provenance tools onto a new MCP server backed by svc. Pass a
// non-nil logger to surface server activity on stderr.
func New(svc *app.Service, logger *slog.Logger) *Server {
	opts := &mcp.ServerOptions{}
	if logger != nil {
		opts.Logger = logger
	}
	srv := mcp.NewServer(Implementation, opts)
	verifier := provenance.Verifier{Git: git.Reader{Root: svc.Root}, Store: svc.Store}
	registerTools(srv, svc, verifier)
	return &Server{sdk: srv, svc: svc}
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
	svc := &app.Service{Root: root, MaxFileBytes: cfg.MaxFileBytes, Store: store}
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
