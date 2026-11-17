package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

type gcClient struct {
	base, user, password, token string
	http                        *http.Client
}

func newGCClient(base, user, password string) *gcClient {
	return &gcClient{base: strings.TrimSuffix(base, "/"), user: user, password: password, http: &http.Client{Timeout: 15 * time.Second}}
}

func (g *gcClient) login(ctx context.Context) error {
	var session struct {
		Token string `json:"token"`
	}
	code, body, err := g.do(ctx, http.MethodPost, "/login", map[string]string{"username": g.user, "password": g.password})
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if code != http.StatusOK {
		return fmt.Errorf("login: Ground Control answered %d: %s", code, body)
	}
	if err := json.Unmarshal(body, &session); err != nil || session.Token == "" {
		return errors.Join(errors.New("login: no token in the response"), err)
	}
	g.token = session.Token
	return nil
}

// do sends body as JSON (when not nil) with the session token and returns
// the status and the response body.
func (g *gcClient) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, reader) //nolint:gosec // the E2E Ground Control URL is the operator's -gc-url
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.http.Do(req) //nolint:gosec // see NewRequestWithContext above
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw, err
}

// getJSON decodes a 200 answer of path into out.
func (g *gcClient) getJSON(ctx context.Context, path string, out any) error {
	code, body, err := g.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("GET %s answered %d: %s", path, code, body)
	}
	return json.Unmarshal(body, out)
}

type lifecycle struct {
	Name                    string `json:"name"`
	State                   string `json:"state"`
	LastSeen                string `json:"last_seen"`
	RevokedAt               string `json:"revoked_at"`
	PublishedArtifactDigest string `json:"published_artifact_digest"`
	AppliedStateDigest      string `json:"applied_state_digest"`
	Converged               bool   `json:"converged"`
	PublishError            string `json:"publish_error"`
	AppliedStates           []struct {
		Ref             string `json:"ref"`
		Digest          string `json:"digest"`
		PublishedDigest string `json:"published_digest"`
	} `json:"applied_states"`
}

func (g *gcClient) lifecycle(ctx context.Context, satellite string) (lifecycle, error) {
	var l lifecycle
	err := g.getJSON(ctx, "/api/satellites/"+url.PathEscape(satellite)+"/lifecycle", &l)
	return l, err
}

func (l lifecycle) String() string {
	raw, err := json.Marshal(l)
	if err != nil {
		type plain lifecycle
		return fmt.Sprintf("%+v", plain(l))
	}
	return string(raw)
}

// containerLogs returns the logs of container since t, or all logs when t
// is zero.
func containerLogs(ctx context.Context, container string, since time.Time) (string, error) {
	args := []string{"logs"}
	if !since.IsZero() {
		args = append(args, "--since", since.UTC().Format(time.RFC3339Nano))
	}
	out, err := exec.CommandContext(ctx, "docker", append(args, container)...).CombinedOutput() //nolint:gosec // container names come from the Taskfile
	if err != nil {
		return "", fmt.Errorf("docker logs %s: %w: %s", container, err, out)
	}
	return string(out), nil
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}
