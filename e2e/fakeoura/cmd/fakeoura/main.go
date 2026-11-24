// Command fakeoura runs the fake Oura API for the end-to-end tests.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/rkennel/oura-mcp/e2e/fakeoura"
	"github.com/rkennel/oura-mcp/internal/app"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fake Oura API stopped with an error", "error", err)
		os.Exit(1)
	}
}

// run serves the fake until the process receives SIGINT or SIGTERM. It
// shares the server's serving lifecycle, so the fake shuts down the same
// way.
func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	config, err := fakeoura.LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", config.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.Addr, err)
	}
	handler := fakeoura.NewHandler(fakeoura.NewState(), config.Client)
	return app.Serve(ctx, listener, handler, logger)
}
