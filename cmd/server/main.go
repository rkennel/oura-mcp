// Command server runs the Oura MCP server.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rkennel/oura-mcp/internal/app"
)

// version is the build's version, reported to MCP clients. Release builds
// set it with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped with an error", "error", err)
		os.Exit(1)
	}
}

// run serves until the process receives SIGINT or SIGTERM. It is separate
// from main so that its deferred cleanup runs before the process exits.
func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return app.Run(ctx, os.Getenv, logger, version)
}
