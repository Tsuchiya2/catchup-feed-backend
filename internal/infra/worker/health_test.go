package worker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestHealthServer_Liveness(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := NewHealthServer("localhost:19091", logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	go func() {
		if err := server.Start(ctx); err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test liveness endpoint - should always return 200
	resp, err := http.Get("http://localhost:19091/health")
	if err != nil {
		t.Fatalf("failed to call /health: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var response healthResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", response.Status)
	}

	// Stop server
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestHealthServer_Readiness_NotReady(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := NewHealthServer("localhost:19092", logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background (not ready by default)
	go func() {
		if err := server.Start(ctx); err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test readiness endpoint - should return 503 when not ready
	resp, err := http.Get("http://localhost:19092/health/ready")
	if err != nil {
		t.Fatalf("failed to call /health/ready: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var response healthResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Status != "not ready" {
		t.Errorf("expected status 'not ready', got '%s'", response.Status)
	}

	// Stop server
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestHealthServer_Readiness_Ready(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := NewHealthServer("localhost:19093", logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	go func() {
		if err := server.Start(ctx); err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Mark as ready
	server.SetReady(true)

	// Test readiness endpoint - should return 200 when ready
	resp, err := http.Get("http://localhost:19093/health/ready")
	if err != nil {
		t.Fatalf("failed to call /health/ready: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var response healthResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", response.Status)
	}

	// Stop server
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestHealthServer_Readiness_Transition(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := NewHealthServer("localhost:19094", logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	go func() {
		if err := server.Start(ctx); err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test 1: Not ready initially
	resp, err := http.Get("http://localhost:19094/health/ready")
	if err != nil {
		t.Fatalf("failed to call /health/ready: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 initially, got %d", resp.StatusCode)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("failed to close response body: %v", err)
	}

	// Test 2: Transition to ready
	server.SetReady(true)
	time.Sleep(10 * time.Millisecond)

	resp, err = http.Get("http://localhost:19094/health/ready")
	if err != nil {
		t.Fatalf("failed to call /health/ready after SetReady: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 after SetReady(true), got %d", resp.StatusCode)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("failed to close response body: %v", err)
	}

	// Test 3: Transition back to not ready
	server.SetReady(false)
	time.Sleep(10 * time.Millisecond)

	resp, err = http.Get("http://localhost:19094/health/ready")
	if err != nil {
		t.Fatalf("failed to call /health/ready after SetReady(false): %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 after SetReady(false), got %d", resp.StatusCode)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("failed to close response body: %v", err)
	}

	// Stop server
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestHealthServer_GracefulShutdown(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := NewHealthServer("localhost:19095", logger)

	ctx, cancel := context.WithCancel(context.Background())

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		if err := server.Start(ctx); err != nil {
			errChan <- err
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	resp, err := http.Get("http://localhost:19095/health")
	if err != nil {
		t.Fatalf("server not running: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("failed to close response body: %v", err)
	}

	// Trigger graceful shutdown
	cancel()

	// Wait for shutdown to complete
	select {
	case err := <-errChan:
		if err != http.ErrServerClosed {
			t.Errorf("expected http.ErrServerClosed, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown timeout")
	}

	// Verify server is stopped
	_, err = http.Get("http://localhost:19095/health")
	if err == nil {
		t.Error("expected connection error after shutdown, but got success")
	}
}

func TestNewHealthServer(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := NewHealthServer(":9091", logger)

	if server.addr != ":9091" {
		t.Errorf("expected addr ':9091', got '%s'", server.addr)
	}

	if server.logger == nil {
		t.Error("expected logger to be set")
	}

	if server.isReady == nil {
		t.Fatal("expected isReady to be initialized")
	}

	// Should start as not ready
	if server.isReady.Load() {
		t.Error("expected isReady to be false initially")
	}
}

func TestSetReady(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := NewHealthServer(":9091", logger)

	// Initially not ready
	if server.isReady.Load() {
		t.Error("expected isReady to be false initially")
	}

	// Set to ready
	server.SetReady(true)
	if !server.isReady.Load() {
		t.Error("expected isReady to be true after SetReady(true)")
	}

	// Set back to not ready
	server.SetReady(false)
	if server.isReady.Load() {
		t.Error("expected isReady to be false after SetReady(false)")
	}
}
