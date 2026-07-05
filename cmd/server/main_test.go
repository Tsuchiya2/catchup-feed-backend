package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitGet retries an HTTP GET until the server is reachable or the
// deadline passes (the listener goroutine may not have called Serve yet).
// The per-request timeout keeps a non-responding server a fast test
// failure instead of hanging until the go test global timeout.
func waitGet(t *testing.T, url string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := client.Get(url) //nolint:gosec // test-local loopback URL
		if err == nil {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s never succeeded: %v", url, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestStartPrivateFeedListener covers the degraded and normal startup
// paths of the tailnet-only listener (§3.1, §8).
func TestStartPrivateFeedListener(t *testing.T) {
	tests := []struct {
		name string
		// addr returns the listen address to pass in; cleanup (if any)
		// is registered on t.
		addr     func(t *testing.T) string
		wantSrv  bool
		checkSrv func(t *testing.T, srv *http.Server)
	}{
		{
			name: "bind failure degrades to nil (no crash)",
			addr: func(t *testing.T) string {
				// Occupy a port so the bind is guaranteed to fail,
				// simulating tailscaled being down / addr in use.
				blocker, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				t.Cleanup(func() { _ = blocker.Close() })
				return blocker.Addr().String()
			},
			wantSrv: false,
		},
		{
			name:    "successful bind serves the private handler",
			addr:    func(_ *testing.T) string { return "127.0.0.1:0" },
			wantSrv: true,
			checkSrv: func(t *testing.T, srv *http.Server) {
				resp := waitGet(t, "http://"+srv.Addr+"/")
				defer func() { _ = resp.Body.Close() }()
				assert.Equal(t, http.StatusTeapot, resp.StatusCode)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			})

			srv := startPrivateFeedListener(ctx, testLogger(), tt.addr(t), handler)

			if !tt.wantSrv {
				assert.Nil(t, srv)
				return
			}
			require.NotNil(t, srv)
			t.Cleanup(func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
				defer shutdownCancel()
				assert.NoError(t, srv.Shutdown(shutdownCtx))
			})
			if tt.checkSrv != nil {
				tt.checkSrv(t, srv)
			}
		})
	}
}

// TestPrivateBindFailureKeepsPublicServing is the §8 regression test:
// when the private listener cannot bind (e.g. tailscaled is down), the
// public server must keep serving instead of being torn down with it.
func TestPrivateBindFailureKeepsPublicServing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stand-in for the public server on a real socket.
	publicLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	publicSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = publicSrv.Serve(publicLn) }()
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = publicSrv.Shutdown(shutdownCtx)
	})

	// Force the private bind to fail by pointing it at an occupied port.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Close() })

	privateSrv := startPrivateFeedListener(ctx, testLogger(), blocker.Addr().String(),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	// 縮退: private は諦める(nil)が、公開側は生きたまま応答する。
	assert.Nil(t, privateSrv)

	resp := waitGet(t, "http://"+publicLn.Addr().String()+"/")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
