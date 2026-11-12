package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// typePrefix is webhook.TypePrefix; GC kinds become CloudEvent types under it.
const typePrefix = "io.harborsatellite.groundcontrol."

func webhookCreateFlags(fs *flag.FlagSet) func(context.Context, *gcClient) error {
	target := fs.String("url", "", "receiver URL as Ground Control's dispatcher reaches it")
	events := fs.String("events", "", "comma-separated event kinds; empty subscribes to all")
	return func(ctx context.Context, g *gcClient) error {
		secret := os.Getenv("WEBHOOK_SECRET")
		if *target == "" || secret == "" {
			return errors.New("-url and WEBHOOK_SECRET are required")
		}
		req := map[string]any{"url": *target, "secret": secret, "event_types": splitList(*events)}
		code, body, err := g.do(ctx, http.MethodPost, "/api/webhooks", req)
		if err != nil {
			return err
		}
		if code != http.StatusCreated {
			return fmt.Errorf("POST /api/webhooks answered %d: %s", code, body)
		}
		fmt.Printf("PASS: webhook subscription created: %s\n", body)
		return nil
	}
}

type delivery struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Subject        string    `json:"subject"`
	IdempotencyKey string    `json:"idempotency_key"`
	SignatureValid bool      `json:"signature_valid"`
	ReceivedAt     time.Time `json:"received_at"`
}

// webhookDeliverFlags waits until the receiver holds at least the wanted
// number of distinct events per kind, waits -settle for redeliveries, then
// requires each event ID exactly once, a valid signature on every delivery
// and the event ID as Idempotency-Key.
func webhookDeliverFlags(fs *flag.FlagSet) func(context.Context, *gcClient) error {
	container := fs.String("receiver-container", "", "container of the webhook receiver")
	receiverURL := fs.String("receiver-url", "http://127.0.0.1:9999/deliveries", "deliveries URL inside the receiver container")
	want := fs.String("want", "", "kind=minimum distinct events, comma-separated, e.g. GroupChanged=2,SatelliteStale=1")
	settle := fs.Duration("settle", 20*time.Second, "time to wait for redeliveries after the wanted events arrived")
	interval := fs.Duration("interval", 3*time.Second, "time between polls")
	return func(ctx context.Context, _ *gcClient) error {
		wanted, err := parseWant(*want)
		if err != nil || *container == "" {
			return errors.Join(errors.New("-receiver-container and -want are required"), err)
		}
		read := func() ([]delivery, error) { return readDeliveries(ctx, *container, *receiverURL) }
		var got []delivery
		err = poll(ctx, *interval, "webhook deliveries "+*want, func() (bool, error) {
			if got, err = read(); err != nil {
				return false, err
			}
			missing := missingKinds(got, wanted)
			if len(missing) == 0 {
				return true, nil
			}
			return false, fmt.Errorf("still missing %s", strings.Join(missing, ", "))
		})
		if err != nil {
			return fmt.Errorf("%w; received %s", err, summary(got))
		}
		fmt.Printf("Received %s; waiting %s for redeliveries...\n", summary(got), *settle)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(*settle):
		}
		if got, err = read(); err != nil {
			return err
		}
		if err := exactlyOnce(got); err != nil {
			return err
		}
		fmt.Printf("PASS: %d webhook deliveries, each event ID exactly once, all signed: %s\n", len(got), summary(got))
		return nil
	}
}

func readDeliveries(ctx context.Context, container, url string) ([]delivery, error) {
	out, err := exec.CommandContext(ctx, "docker", "exec", container, "wget", "-qO-", url).Output() //nolint:gosec // arguments come from the Taskfile
	if err != nil {
		return nil, fmt.Errorf("read deliveries from %s: %w", container, err)
	}
	var got []delivery
	if err := json.Unmarshal(out, &got); err != nil {
		return nil, fmt.Errorf("decode deliveries: %w", err)
	}
	return got, nil
}

func exactlyOnce(got []delivery) error {
	seen := map[string]int{}
	var errs []error
	for _, d := range got {
		seen[d.ID]++
		if !d.SignatureValid {
			errs = append(errs, fmt.Errorf("event %s (%s) has an invalid signature", d.ID, d.Type))
		}
		if d.IdempotencyKey != d.ID {
			errs = append(errs, fmt.Errorf("event %s has Idempotency-Key %q", d.ID, d.IdempotencyKey))
		}
	}
	for id, n := range seen {
		if n != 1 {
			errs = append(errs, fmt.Errorf("event %s was delivered %d times", id, n))
		}
	}
	return errors.Join(errs...)
}

// missingKinds lists the kinds with fewer distinct event IDs than wanted.
func missingKinds(got []delivery, wanted map[string]int) []string {
	ids := map[string]map[string]bool{}
	for _, d := range got {
		if ids[d.Type] == nil {
			ids[d.Type] = map[string]bool{}
		}
		ids[d.Type][d.ID] = true
	}
	var missing []string
	for kind, n := range wanted {
		if have := len(ids[typePrefix+kind]); have < n {
			missing = append(missing, fmt.Sprintf("%s (%d of %d)", kind, have, n))
		}
	}
	sort.Strings(missing)
	return missing
}

func summary(got []delivery) string {
	counts := map[string]int{}
	for _, d := range got {
		counts[strings.TrimPrefix(d.Type, typePrefix)]++
	}
	parts := make([]string, 0, len(counts))
	for kind, n := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", kind, n))
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, " ")
}

func parseWant(s string) (map[string]int, error) {
	wanted := map[string]int{}
	for _, item := range splitList(s) {
		kind, count, ok := strings.Cut(item, "=")
		n, err := strconv.Atoi(count)
		if !ok || err != nil || n < 1 {
			return nil, fmt.Errorf("-want item %q: want kind=count", item)
		}
		wanted[kind] = n
	}
	if len(wanted) == 0 {
		return nil, errors.New("-want is empty")
	}
	return wanted, nil
}

func splitList(s string) []string {
	out := []string{}
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
