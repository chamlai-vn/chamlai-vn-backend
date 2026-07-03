package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server never started listening on %s", addr)
}

func TestRun_ServesUntilContextCancelled(t *testing.T) {
	addr := freeAddr(t)
	srv := NewServer(Config{Addr: addr}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- Run(ctx, srv) }()
	waitForServer(t, addr)

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestRun_DrainsInFlightRequestOnShutdown is the graceful-shutdown success
// criterion from the plan: a request already inside the handler when ctx is
// cancelled must complete, not be cut off.
func TestRun_DrainsInFlightRequestOnShutdown(t *testing.T) {
	addr := freeAddr(t)
	started := make(chan struct{})
	release := make(chan struct{})

	srv := NewServer(Config{Addr: addr}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	}))

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- Run(ctx, srv) }()
	waitForServer(t, addr)

	reqDone := make(chan *http.Response, 1)
	reqErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			reqErr <- err
			return
		}
		reqDone <- resp
	}()

	<-started // request is now in-flight, blocked inside the handler
	cancel()  // ask the server to shut down while the handler is still running
	time.Sleep(50 * time.Millisecond)
	close(release) // let the handler finish

	select {
	case err := <-reqErr:
		t.Fatalf("in-flight request failed instead of draining: %v", err)
	case resp := <-reqDone:
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "done" {
			t.Errorf("body = %q, want request to complete despite shutdown", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight request never completed")
	}

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after drain")
	}
}
