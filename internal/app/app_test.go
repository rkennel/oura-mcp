package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeListener is a net.Listener that never opens a socket. Accept fails
// with acceptErr, as a broken or closed listener would.
type fakeListener struct {
	acceptErr error
}

func (l fakeListener) Accept() (net.Conn, error) { return nil, l.acceptErr }
func (l fakeListener) Close() error              { return nil }
func (l fakeListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// fakeServer stands in for *http.Server. Serve fails at once with serveErr
// when it is set; otherwise it blocks until Shutdown and then returns
// afterShutdown, or http.ErrServerClosed when that is nil.
type fakeServer struct {
	serveErr      error
	shutdownErr   error
	afterShutdown error
	stopped       chan struct{}
	shutdownCalls int
}

func newFakeServer() *fakeServer {
	return &fakeServer{stopped: make(chan struct{})}
}

func (s *fakeServer) Serve(net.Listener) error {
	if s.serveErr != nil {
		return s.serveErr
	}
	<-s.stopped
	if s.afterShutdown != nil {
		return s.afterShutdown
	}
	return http.ErrServerClosed
}

func (s *fakeServer) Shutdown(context.Context) error {
	s.shutdownCalls++
	close(s.stopped)
	return s.shutdownErr
}

// serveInBackground runs serveUntilCancelled on its own goroutine and
// returns a channel that receives its result.
func serveInBackground(ctx context.Context, server httpServer) <-chan error {
	done := make(chan error, 1)
	go func() { done <- serveUntilCancelled(ctx, server, fakeListener{}, discardLogger()) }()
	return done
}

func waitFor(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("serveUntilCancelled did not return")
		return nil
	}
}

func TestServeUntilCancelledShutsDownCleanlyWhenCancelled(t *testing.T) {
	server := newFakeServer()
	ctx, cancel := context.WithCancel(context.Background())
	done := serveInBackground(ctx, server)

	cancel()

	if err := waitFor(t, done); err != nil {
		t.Errorf("returned an error after cancellation: %v", err)
	}
	if server.shutdownCalls != 1 {
		t.Errorf("Shutdown called %d times, want 1", server.shutdownCalls)
	}
}

func TestServeUntilCancelledReportsAServeFailure(t *testing.T) {
	failure := errors.New("serve failed")
	server := newFakeServer()
	server.serveErr = failure

	err := waitFor(t, serveInBackground(context.Background(), server))

	if !errors.Is(err, failure) {
		t.Errorf("error = %v, want it to wrap %v", err, failure)
	}
	if server.shutdownCalls != 0 {
		t.Errorf("Shutdown called %d times after a serve failure, want 0", server.shutdownCalls)
	}
}

func TestServeUntilCancelledReportsAShutdownFailure(t *testing.T) {
	failure := errors.New("shutdown failed")
	server := newFakeServer()
	server.shutdownErr = failure
	ctx, cancel := context.WithCancel(context.Background())
	done := serveInBackground(ctx, server)

	cancel()

	if err := waitFor(t, done); !errors.Is(err, failure) {
		t.Errorf("error = %v, want it to wrap %v", err, failure)
	}
}

func TestServeUntilCancelledReportsAnUnexpectedErrorAfterShutdown(t *testing.T) {
	failure := errors.New("connection reset")
	server := newFakeServer()
	server.afterShutdown = failure
	ctx, cancel := context.WithCancel(context.Background())
	done := serveInBackground(ctx, server)

	cancel()

	if err := waitFor(t, done); !errors.Is(err, failure) {
		t.Errorf("error = %v, want it to wrap %v", err, failure)
	}
}

func TestServeReportsAListenerFailure(t *testing.T) {
	failure := errors.New("accept failed")

	err := Serve(context.Background(), fakeListener{acceptErr: failure}, discardLogger())

	if !errors.Is(err, failure) {
		t.Errorf("error = %v, want it to wrap %v", err, failure)
	}
}

func TestNewServerSetsTheRoutesAndTimeouts(t *testing.T) {
	server := newServer()

	if server.Handler == nil {
		t.Error("Handler is nil")
	}
	timeouts := []struct {
		name      string
		got, want time.Duration
	}{
		{"ReadHeaderTimeout", server.ReadHeaderTimeout, readHeaderTimeout},
		{"ReadTimeout", server.ReadTimeout, readTimeout},
		{"WriteTimeout", server.WriteTimeout, writeTimeout},
		{"IdleTimeout", server.IdleTimeout, idleTimeout},
	}
	for _, timeout := range timeouts {
		if timeout.got != timeout.want {
			t.Errorf("%s = %v, want %v", timeout.name, timeout.got, timeout.want)
		}
	}
}

// unexpectedListen and unexpectedServe fail the test if run reaches them.
func unexpectedListen(t *testing.T) listenFunc {
	return func(string, string) (net.Listener, error) {
		t.Error("listen was called")
		return nil, errors.New("unexpected listen")
	}
}

func unexpectedServe(t *testing.T) serveFunc {
	return func(context.Context, net.Listener, *slog.Logger) error {
		t.Error("serve was called")
		return errors.New("unexpected serve")
	}
}

func TestRunRejectsInvalidConfigurationBeforeListening(t *testing.T) {
	getenv := envOf(map[string]string{"PORT": "nope"})

	err := run(context.Background(), getenv, discardLogger(), unexpectedListen(t), unexpectedServe(t))

	if err == nil {
		t.Error("run returned nil for an invalid PORT")
	}
}

func TestRunReportsAListenFailureWithoutServing(t *testing.T) {
	failure := errors.New("address already in use")
	listen := func(string, string) (net.Listener, error) { return nil, failure }

	err := run(context.Background(), envOf(nil), discardLogger(), listen, unexpectedServe(t))

	if !errors.Is(err, failure) {
		t.Errorf("error = %v, want it to wrap %v", err, failure)
	}
}

func TestRunListensOnTheConfiguredAddress(t *testing.T) {
	var network, address string
	listen := func(n, a string) (net.Listener, error) {
		network, address = n, a
		return fakeListener{}, nil
	}
	serve := func(context.Context, net.Listener, *slog.Logger) error { return nil }

	if err := run(context.Background(), envOf(map[string]string{"PORT": "9090"}), discardLogger(), listen, serve); err != nil {
		t.Fatalf("run returned an error: %v", err)
	}

	if network != "tcp" || address != ":9090" {
		t.Errorf("listen(%q, %q), want listen(%q, %q)", network, address, "tcp", ":9090")
	}
}

func TestRunServesOnTheListenerWithTheCallersContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "caller")
	listener := fakeListener{}
	listen := func(string, string) (net.Listener, error) { return listener, nil }
	outcome := errors.New("serve outcome")
	var servedCtx context.Context
	var servedListener net.Listener
	serve := func(c context.Context, l net.Listener, _ *slog.Logger) error {
		servedCtx, servedListener = c, l
		return outcome
	}

	err := run(ctx, envOf(nil), discardLogger(), listen, serve)

	if !errors.Is(err, outcome) {
		t.Errorf("error = %v, want serve's result %v", err, outcome)
	}
	if servedCtx != ctx {
		t.Error("serve did not receive the caller's context")
	}
	if servedListener != listener {
		t.Error("serve did not receive the listener run opened")
	}
}
