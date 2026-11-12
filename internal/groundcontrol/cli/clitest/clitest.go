// Package clitest runs groundcontrol CLI commands against a recording
// Ground Control, for tests of the command packages.
package clitest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli"
)

// Request is what the CLI sent.
type Request struct {
	Method, Path, RawPath, Query, Authorization string
	Body                                        map[string]any
}

// Server answers every request with Status and Body and records requests.
type Server struct {
	URL string

	mu       sync.Mutex
	requests []Request
	status   int
	body     string
}

// NewServer starts a recording server that answers status and body.
func NewServer(t testing.TB, status int, body string) *Server {
	t.Helper()
	s := &Server{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		got := Request{
			Method: r.Method, Path: r.URL.Path, RawPath: r.URL.EscapedPath(), Query: r.URL.RawQuery,
			Authorization: r.Header.Get("Authorization"),
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &got.Body); err != nil {
				t.Errorf("request body is not a JSON object: %v: %s", err, raw)
			}
		}
		s.mu.Lock()
		s.requests = append(s.requests, got)
		status, body := s.status, s.body
		s.mu.Unlock()
		if body != "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// Requests returns the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// Run executes the groundcontrol CLI with args and a credentials file of
// the test's own, and returns what it printed.
func Run(t testing.TB, stdin io.Reader, args ...string) (string, error) {
	t.Helper()
	t.Setenv("GROUND_CONTROL_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials.json"))
	out := &bytes.Buffer{}
	cmd := cli.RootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	if stdin != nil {
		cmd.SetIn(stdin)
	}
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

// RunAuthenticated runs args against server with a session token.
func RunAuthenticated(t testing.TB, server *Server, stdin io.Reader, args ...string) (string, error) {
	t.Helper()
	return Run(t, stdin, append([]string{"--server", server.URL, "--token", "session-token"}, args...)...)
}
