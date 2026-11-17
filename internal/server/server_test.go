package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a fake llama-server: the tests re-run this binary with
// BSM_FAKE_SERVER set, so no model or llama.cpp install is needed.
func TestMain(m *testing.M) {
	if mode := os.Getenv("BSM_FAKE_SERVER"); mode != "" {
		fakeServer(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeServer(mode string) {
	var port string
	for i, a := range os.Args {
		if a == "--port" {
			port = os.Args[i+1]
		}
	}
	if mode == "exit" {
		fmt.Fprintln(os.Stderr, "error: failed to load model")
		os.Exit(3)
	}
	start := time.Now()
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if mode == "hang" || time.Since(start) < 300*time.Millisecond {
			http.Error(w, "loading", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	http.ListenAndServe("127.0.0.1:"+port, nil)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func start(t *testing.T, mode string, port int, timeout time.Duration) (*Server, error, string) {
	t.Helper()
	t.Setenv("BSM_FAKE_SERVER", mode)
	log := filepath.Join(t.TempDir(), "server.log")
	s, err := Start(context.Background(), os.Args[0], []string{"-hf", "x/y:Q4"}, port, log, timeout)
	return s, err, log
}

func portFree(port int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func TestStartWaitsForHealthThenStopFreesThePort(t *testing.T) {
	port := freePort(t)
	s, err, _ := start(t, "ok", port, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(s.URL() + "/health")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("health: %v %v", resp, err)
	}
	resp.Body.Close()

	s.Stop()
	s.Stop()
	if !s.Exited() {
		t.Fatal("process still running after Stop")
	}
	if !portFree(port) {
		t.Fatal("port still in use after Stop")
	}
}

func TestExitBeforeReadyNamesTheLog(t *testing.T) {
	s, err, log := start(t, "exit", freePort(t), 10*time.Second)
	if err == nil || s != nil {
		t.Fatalf("want error, got %v", err)
	}
	if !strings.Contains(err.Error(), "exited") || !strings.Contains(err.Error(), log) {
		t.Fatalf("error should say exited and name the log: %v", err)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "failed to load model") {
		t.Fatalf("log = %q", data)
	}
}

func TestNeverHealthyTimesOutAndKillsTheProcess(t *testing.T) {
	port := freePort(t)
	_, err, _ := start(t, "hang", port, 1*time.Second)
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("got %v", err)
	}
	if !portFree(port) {
		t.Fatal("hung server was not killed")
	}
}

func TestPortInUseIsRefused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	_, err, _ = start(t, "ok", port, time.Second)
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("got %v", err)
	}
}

func TestMissingBinaryIsClear(t *testing.T) {
	_, err := Start(context.Background(), "definitely-not-llama-server", nil, freePort(t), filepath.Join(t.TempDir(), "l.log"), time.Second)
	if err == nil || !strings.Contains(err.Error(), "on PATH") {
		t.Fatalf("got %v", err)
	}
}
