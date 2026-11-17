// Package server starts and stops a llama-server process so the runner can
// load one model at a time.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

type Server struct {
	cmd  *exec.Cmd
	done chan struct{}
	log  *os.File
	url  string
	once sync.Once
}

func (s *Server) URL() string { return s.url }

// Start launches bin with --host and --port appended to args, then waits until
// /health answers 200 (503 means the model is still loading). Output goes to
// logPath, which every error names. It refuses a port that is already in use:
// a health check against someone else's server would pass and the run would
// silently use the wrong model.
func Start(ctx context.Context, bin string, args []string, port int, logPath string, timeout time.Duration) (*Server, error) {
	if l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err != nil {
		return nil, fmt.Errorf("port %d is already in use; stop the other server or change the port", port)
	} else {
		l.Close()
	}
	log, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, append(append([]string{}, args...), "--host", "127.0.0.1", "--port", strconv.Itoa(port))...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, fmt.Errorf("start %s: %w (is llama-server on PATH?)", bin, err)
	}
	s := &Server{cmd: cmd, done: make(chan struct{}), log: log, url: "http://127.0.0.1:" + strconv.Itoa(port)}
	go func() {
		cmd.Wait()
		close(s.done)
	}()

	if err := s.waitHealthy(ctx, timeout); err != nil {
		s.Stop()
		return nil, fmt.Errorf("%w (server log: %s)", err, logPath)
	}
	return s, nil
}

func (s *Server) waitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.After(timeout)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		select {
		case <-s.done:
			return errors.New("llama-server exited before it became ready")
		case <-deadline:
			return fmt.Errorf("llama-server not ready after %s", timeout)
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			resp, err := client.Get(s.url + "/health")
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

// Exited reports whether the process has already ended, for a crash mid-run.
func (s *Server) Exited() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Stop kills the process by PID and waits for it. Safe to call more than once.
func (s *Server) Stop() {
	s.once.Do(func() {
		s.cmd.Process.Kill()
		<-s.done
		s.log.Close()
	})
}
