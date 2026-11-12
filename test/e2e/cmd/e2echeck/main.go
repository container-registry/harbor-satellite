// Command e2echeck holds the Ground Control checks of the E2E flows that
// need more than curl: the Harbor outage (GC serves while Harbor is down,
// then converges), satellite revocation and webhook delivery. Each check is
// a subcommand; on failure it prints the reason and exits 1.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"
)

// A check registers its flags and returns the function that runs it.
type check struct {
	usage string
	flags func(fs *flag.FlagSet) func(ctx context.Context, g *gcClient) error
}

var checks = map[string]check{
	"harbor-down":     {"GC serves and accepts writes while Harbor is unreachable; nothing is published", harborDownFlags},
	"converged":       {"wait until a satellite's state is published and applied", convergedFlags},
	"revoke":          {"a revoked satellite is answered 403 and recovers after unrevoke", revokeFlags},
	"webhook-create":  {"subscribe the receiver to fleet events", webhookCreateFlags},
	"webhook-deliver": {"wait for webhook deliveries and require each event ID exactly once", webhookDeliverFlags},
	"spiffe-setup":    {"give a SPIFFE-registered satellite a config and a group", spiffeSetupFlags},
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	c, ok := checks[os.Args[1]]
	if !ok {
		usage()
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	gcURL := fs.String("gc-url", "", "Ground Control URL reachable from the host")
	user := fs.String("user", "admin", "Ground Control admin user")
	timeout := fs.Duration("timeout", 10*time.Minute, "overall deadline of the check")
	insecure := fs.Bool("insecure", false, "skip TLS verification of Ground Control")
	runCheck := c.flags(fs)
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	password := os.Getenv("GC_ADMIN_PASSWORD")
	if *gcURL == "" || password == "" {
		fmt.Fprintln(os.Stderr, "e2echeck: -gc-url and GC_ADMIN_PASSWORD are required")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	g := newGCClient(*gcURL, *user, password)
	if *insecure {
		g.http.Transport = &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // the SPIFFE E2E Ground Control serves its SVID, not a CA-signed certificate
		}}
	}
	err := run(ctx, g, runCheck)
	cancel()
	stop()
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, g *gcClient, runCheck func(context.Context, *gcClient) error) error {
	if err := g.login(ctx); err != nil {
		return err
	}
	return runCheck(ctx, g)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: e2echeck <check> -gc-url URL [-user admin] [-timeout 10m] [check flags]")
	names := make([]string, 0, len(checks))
	for name := range checks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(os.Stderr, "  %-16s %s\n", name, checks[name].usage)
	}
	os.Exit(2)
}

// poll calls probe every interval until it reports done or ctx ends; the
// last probe error is part of the timeout error.
func poll(ctx context.Context, interval time.Duration, what string, probe func() (bool, error)) error {
	var last error
	for {
		done, err := probe()
		if done {
			return nil
		}
		if err != nil {
			last = err
		}
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("timed out waiting for %s: %w", what, last)
			}
			return fmt.Errorf("timed out waiting for %s", what)
		case <-time.After(interval):
		}
	}
}
