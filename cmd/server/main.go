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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := app.Run(ctx, os.Getenv, logger); err != nil {
		logger.Error("server stopped with an error", "error", err)
		os.Exit(1)
	}
}
