package app

import (
	"context"
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

func TestServeAnswersRequestsUntilCancelled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, listener, discardLogger()) }()

	response, err := http.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned an error after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}
}

func TestServeReportsAListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	listener.Close()

	if err := Serve(context.Background(), listener, discardLogger()); err == nil {
		t.Error("Serve returned nil for a closed listener")
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	err := Run(context.Background(), envOf(map[string]string{"PORT": "nope"}), discardLogger())

	if err == nil {
		t.Error("Run returned nil for an invalid PORT")
	}
}

func TestRunReportsAnAddressAlreadyInUse(t *testing.T) {
	occupied, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer occupied.Close()
	_, port, _ := net.SplitHostPort(occupied.Addr().String())

	err = Run(context.Background(), envOf(map[string]string{"PORT": port}), discardLogger())

	if err == nil {
		t.Error("Run returned nil although the port was in use")
	}
}

func TestRunStopsCleanlyWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := Run(ctx, envOf(map[string]string{"PORT": "0"}), discardLogger()); err != nil {
		t.Errorf("Run returned an error after cancellation: %v", err)
	}
}
