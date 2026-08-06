// Command ai-prov-mcp runs the stdio Model Context Protocol server.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ai-prov/internal/mcp"
)

// Build metadata injected via -ldflags by the Makefile. Defaults keep the
// binary runnable when built without the release target.
var (
	version = "development"
	commit  = ""
	builtAt = ""
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mcp.Implementation.Version = versionOrElse(version, "development")
	srv, err := mcp.BootstrapFromEnvironment(logger)
	if err != nil {
		logger.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}
	defer srv.Cleanup()
	logger.Info("ai-prov-mcp starting", "version", version, "commit", commit, "built_at", builtAt)

	if err := srv.Run(ctx); err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

// versionOrElse returns v when non-empty, otherwise fallback. Used so an
// empty -ldflags value does not produce a confusing "empty" version string.
func versionOrElse(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
