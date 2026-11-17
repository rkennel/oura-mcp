// Package app wires the server together and runs it.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/rkennel/oura-mcp/internal/httpapi"
)

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second

	// shutdownTimeout is how long in-flight requests get to finish once the
	// server is told to stop. Cloud Run allows ten seconds after SIGTERM.
	shutdownTimeout = 8 * time.Second
)

// listenFunc opens the listener the server accepts connections on.
// Production code passes net.Listen.
type listenFunc func(network, address string) (net.Listener, error)

// serveFunc serves on listener until ctx is cancelled. Production code passes
// Serve.
type serveFunc func(ctx context.Context, listener net.Listener, logger *slog.Logger) error

// httpServer is the part of *http.Server that the serving lifecycle drives.
type httpServer interface {
	Serve(listener net.Listener) error
	Shutdown(ctx context.Context) error
}

// Run loads configuration, listens on the configured address and serves
// until ctx is cancelled.
func Run(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {
	return run(ctx, getenv, logger, net.Listen, Serve)
}

// run is Run with its network collaborators injected, so that unit tests
// can replace them.
func run(ctx context.Context, getenv func(string) string, logger *slog.Logger, listen listenFunc, serve serveFunc) error {
	config, err := LoadConfig(getenv)
	if err != nil {
		return err
	}

	listener, err := listen("tcp", config.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.Addr, err)
	}

	return serve(ctx, listener, logger)
}

// Serve handles requests on listener until ctx is cancelled, then shuts
// down gracefully. It returns nil after a clean shutdown.
func Serve(ctx context.Context, listener net.Listener, logger *slog.Logger) error {
	return serveUntilCancelled(ctx, newServer(), listener, logger)
}

// newServer returns the HTTP server with the routes and timeouts the
// service runs with.
func newServer() *http.Server {
	return &http.Server{
		Handler:           httpapi.NewHandler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// serveUntilCancelled runs server on listener until ctx is cancelled or
// serving fails, then shuts the server down gracefully.
func serveUntilCancelled(ctx context.Context, server httpServer, listener net.Listener, logger *slog.Logger) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	logger.Info("server listening", "addr", listener.Addr().String())

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("server shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
