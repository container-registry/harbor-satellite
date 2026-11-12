package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// The satellite logs gcclient.ErrForbidden with GC's message.
const revokedMessage = "satellite revoked"

// revokeFlags revokes a running satellite and requires that GC answers its
// next heartbeat with 403 and keeps it revoked, then unrevokes it and
// requires that its next heartbeat brings it online again.
func revokeFlags(fs *flag.FlagSet) func(context.Context, *gcClient) error {
	satellite := fs.String("satellite", "", "satellite name")
	container := fs.String("satellite-container", "", "satellite container whose logs show GC's answer")
	interval := fs.Duration("interval", 5*time.Second, "time between polls")
	return func(ctx context.Context, g *gcClient) error {
		if *satellite == "" || *container == "" {
			return errors.New("-satellite and -satellite-container are required")
		}
		err := revoke(ctx, g, *satellite, *container, *interval)
		if err != nil {
			if logs, lerr := containerLogs(ctx, *container, time.Time{}); lerr == nil {
				fmt.Println("--- satellite logs ---")
				fmt.Println(tail(logs, 30))
			}
		}
		return err
	}
}

func revoke(ctx context.Context, g *gcClient, satellite, container string, interval time.Duration) error {
	path := "/api/satellites/" + url.PathEscape(satellite)
	revokedAt := time.Now()
	if err := lifecycleOp(ctx, g, path+"/revoke", "revoked"); err != nil {
		return err
	}
	fmt.Printf("Revoked %s; waiting for GC to refuse its heartbeat...\n", satellite)
	err := poll(ctx, interval, "the satellite to log GC's 403", func() (bool, error) {
		logs, err := containerLogs(ctx, container, revokedAt)
		return strings.Contains(logs, revokedMessage), err
	})
	if err != nil {
		return err
	}
	fmt.Printf("PASS: GC answered the heartbeat of revoked satellite %s with 403 (%q)\n", satellite, revokedMessage)
	if err := probeSync(ctx, g, container); err != nil {
		return err
	}
	l, err := g.lifecycle(ctx, satellite)
	if err != nil {
		return err
	}
	if l.State != "revoked" {
		return fmt.Errorf("satellite %s is %s after a refused heartbeat, want revoked", satellite, l.State)
	}
	fmt.Printf("PASS: satellite %s stays revoked after its heartbeat\n", satellite)

	if err := lifecycleOp(ctx, g, path+"/unrevoke", "pending"); err != nil {
		return err
	}
	err = poll(ctx, interval, "the unrevoked satellite to come online", func() (bool, error) {
		l, err := g.lifecycle(ctx, satellite)
		if err != nil {
			return false, err
		}
		return l.State == "online", fmt.Errorf("state is %s", l.State)
	})
	if err != nil {
		return err
	}
	fmt.Printf("PASS: satellite %s is online again after unrevoke\n", satellite)
	return nil
}

// probeSync sends a heartbeat with each credential the satellite holds and
// requires 403 for every one: a revoked satellite has no way back in.
func probeSync(ctx context.Context, g *gcClient, container string) error {
	out, err := exec.CommandContext(ctx, "docker", "exec", container, "sh", "-c", //nolint:gosec // container name comes from the Taskfile
		`cat "${CONFIG_DIR:-$HOME/.config/satellite}/config.json"`).Output()
	if err != nil {
		return fmt.Errorf("read the satellite config.json: %w", err)
	}
	var cfg struct {
		StateConfig struct {
			Auth struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"auth"`
			GCSession struct {
				Token string `json:"token"`
			} `json:"gc_session"`
		} `json:"state_config"`
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		return fmt.Errorf("decode the satellite config.json: %w", err)
	}
	creds := map[string]func(*http.Request){}
	if a := cfg.StateConfig.Auth; a.Username != "" {
		creds["robot"] = func(r *http.Request) { r.SetBasicAuth(a.Username, a.Password) }
	}
	if t := cfg.StateConfig.GCSession.Token; t != "" {
		creds["session"] = func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+t) }
	}
	if len(creds) == 0 {
		return errors.New("the satellite config.json holds no credentials")
	}
	for name, authorize := range creds {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.base+"/satellites/sync", strings.NewReader("{}")) //nolint:gosec // the E2E Ground Control URL is the operator's -gc-url
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		authorize(req)
		resp, err := g.http.Do(req) //nolint:gosec // see NewRequestWithContext above
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			return fmt.Errorf("heartbeat with the %s credentials of a revoked satellite answered %s, want 403", name, resp.Status)
		}
		fmt.Printf("PASS: heartbeat with the %s credentials of the revoked satellite answered 403\n", name)
	}
	return nil
}

// lifecycleOp posts a revoke or unrevoke and requires the state it leads to.
func lifecycleOp(ctx context.Context, g *gcClient, path, want string) error {
	code, body, err := g.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("POST %s answered %d: %s", path, code, body)
	}
	if !strings.Contains(string(body), `"state":"`+want+`"`) {
		return fmt.Errorf("POST %s answered %s, want state %s", path, body, want)
	}
	return nil
}
