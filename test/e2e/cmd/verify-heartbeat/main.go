// Command verify-heartbeat waits until Ground Control has recorded two
// consecutive heartbeats of a satellite (E2E plan item 0.5, defect D1), then
// checks the satellite's own view in its /metrics: heartbeats answered 200,
// none refused since, and an accepted journal ack. last_seen alone can move
// while the satellite gets an error. On failure it prints the last status
// and the satellite and GC logs.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

type options struct {
	gcURL, user, password           string
	satellite                       string
	satelliteContainer, gcContainer string
	metricsURL                      string
	insecure, requireJournalAck     bool
	attempts                        int
	interval                        time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.gcURL, "gc-url", "", "Ground Control URL reachable from the host")
	flag.StringVar(&o.user, "user", "admin", "Ground Control admin user")
	flag.StringVar(&o.satellite, "satellite", "", "satellite name")
	flag.StringVar(&o.satelliteContainer, "satellite-container", "", "satellite container, scraped for /metrics and its logs on failure")
	flag.StringVar(&o.metricsURL, "metrics-url", "http://127.0.0.1:8586/metrics", "satellite /metrics URL inside its container")
	flag.BoolVar(&o.insecure, "insecure", false, "skip TLS verification of Ground Control")
	flag.BoolVar(&o.requireJournalAck, "require-journal-ack", true, "require an accepted journal ack; false after a restart with an already acknowledged journal, which uploads nothing")
	flag.StringVar(&o.gcContainer, "gc-container", "", "Ground Control container, for logs on failure")
	flag.IntVar(&o.attempts, "attempts", 24, "status polls before giving up")
	flag.DurationVar(&o.interval, "interval", 5*time.Second, "time between status polls")
	flag.Parse()
	o.password = os.Getenv("GC_ADMIN_PASSWORD")
	if o.gcURL == "" || o.satellite == "" || o.satelliteContainer == "" || o.password == "" {
		fmt.Fprintln(os.Stderr, "verify-heartbeat: -gc-url, -satellite, -satellite-container and GC_ADMIN_PASSWORD are required")
		os.Exit(2)
	}
	err := run(context.Background(), o)
	if err == nil {
		err = verifySatelliteMetrics(context.Background(), o)
	}
	if err != nil {
		fmt.Println("FAIL:", err)
		dumpLogs(o)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options) error {
	client := &http.Client{Timeout: 10 * time.Second}
	if o.insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // E2E Ground Control has a self-signed or SPIFFE certificate.
		}}
	}
	token, err := login(ctx, client, o)
	if err != nil {
		return err
	}
	fmt.Printf("Waiting for two consecutive heartbeats of %s...\n", o.satellite)
	var first, last string
	for i := 1; i <= o.attempts; i++ {
		seen, body, err := lastHeartbeat(ctx, client, o, token)
		last = body
		switch {
		case err != nil:
			fmt.Printf("status not readable yet (%d/%d): %v\n", i, o.attempts, err)
		case seen != "" && first == "":
			first = seen
			fmt.Println("Heartbeat recorded at", first)
		case seen != "" && seen != first:
			fmt.Printf("PASS: heartbeat recorded at %s after %s\n", seen, first)
			return nil
		default:
			fmt.Printf("Waiting for heartbeat... (%d/%d)\n", i, o.attempts)
		}
		time.Sleep(o.interval)
	}
	return fmt.Errorf("no second heartbeat for %s; last status: %s", o.satellite, last)
}

func login(ctx context.Context, client *http.Client, o options) (string, error) {
	payload, err := json.Marshal(map[string]string{"username": o.user, "password": o.password})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(o.gcURL, "/")+"/login", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()
	var session struct {
		Token string `json:"token"`
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login: Ground Control answered %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil || session.Token == "" {
		return "", errors.Join(errors.New("login: no token in the response"), err)
	}
	return session.Token, nil
}

// lastHeartbeat returns the created_at of the latest status record; the API
// has answered both CreatedAt and created_at, so either is accepted.
func lastHeartbeat(ctx context.Context, client *http.Client, o options, token string) (seen, body string, err error) {
	endpoint := strings.TrimSuffix(o.gcURL, "/") + "/api/satellites/" + url.PathEscape(o.satellite) + "/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", string(raw), fmt.Errorf("status answered %s", resp.Status)
	}
	var status struct {
		CreatedAtCamel string `json:"CreatedAt"`
		CreatedAt      string `json:"created_at"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return "", string(raw), err
	}
	if status.CreatedAt != "" {
		return status.CreatedAt, string(raw), nil
	}
	return status.CreatedAtCamel, string(raw), nil
}

func dumpLogs(o options) {
	if o.satelliteContainer != "" {
		fmt.Println("--- satellite status report logs ---")
		for _, line := range strings.Split(containerLogs(o.satelliteContainer), "\n") {
			if strings.Contains(line, `"process":"status_report"`) {
				fmt.Println(line)
			}
		}
	}
	if o.gcContainer != "" {
		fmt.Println("--- Ground Control logs ---")
		lines := strings.Split(strings.TrimSpace(containerLogs(o.gcContainer)), "\n")
		fmt.Println(strings.Join(lines[max(0, len(lines)-20):], "\n"))
	}
}

func containerLogs(container string) string {
	out, err := exec.CommandContext(context.Background(), "docker", "logs", container).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("docker logs %s: %v\n%s", container, err, out)
	}
	return string(out)
}
