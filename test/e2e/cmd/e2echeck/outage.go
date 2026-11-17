package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"
)

type health struct {
	Status     string `json:"status"`
	Components map[string]struct {
		Status string `json:"status"`
		Detail string `json:"detail"`
	} `json:"components"`
}

// harborDownFlags: GC answers /health 200 with Harbor degraded, and the
// satellite registered during the outage has nothing published yet. The
// writes themselves are the Taskfile's init tasks, which fail on any error.
func harborDownFlags(fs *flag.FlagSet) func(context.Context, *gcClient) error {
	satellite := fs.String("satellite", "", "satellite registered during the outage")
	return func(ctx context.Context, g *gcClient) error {
		var h health
		if err := g.getJSON(ctx, "/health", &h); err != nil {
			return fmt.Errorf("GC does not serve /health with Harbor down: %w", err)
		}
		harbor, ok := h.Components["harbor"]
		if h.Status != "degraded" || !ok || harbor.Status != "degraded" {
			return fmt.Errorf("/health with Harbor down is %+v, want degraded with a degraded harbor component", h)
		}
		fmt.Printf("PASS: GC serves /health with Harbor down: %s (%s)\n", h.Status, harbor.Detail)
		if *satellite == "" {
			return nil
		}
		l, err := g.lifecycle(ctx, *satellite)
		if err != nil {
			return err
		}
		if l.PublishedArtifactDigest != "" {
			return fmt.Errorf("satellite %s has a published state while Harbor is down: %s", *satellite, l)
		}
		fmt.Printf("PASS: satellite %s is registered and waits for Harbor (state %s, nothing published)\n", *satellite, l.State)
		return nil
	}
}

// convergedFlags waits for GC to publish the satellite's state and the
// satellite to report it applied, then for /health to be healthy again.
func convergedFlags(fs *flag.FlagSet) func(context.Context, *gcClient) error {
	satellite := fs.String("satellite", "", "satellite name")
	interval := fs.Duration("interval", 5*time.Second, "time between polls")
	return func(ctx context.Context, g *gcClient) error {
		if *satellite == "" {
			return errors.New("-satellite is required")
		}
		start := time.Now()
		var last lifecycle
		err := poll(ctx, *interval, "satellite "+*satellite+" to converge", func() (bool, error) {
			l, err := g.lifecycle(ctx, *satellite)
			if err != nil {
				return false, err
			}
			last = l
			fmt.Printf("lifecycle: state=%s published=%q applied=%q converged=%t publish_error=%q\n",
				l.State, l.PublishedArtifactDigest, l.AppliedStateDigest, l.Converged, l.PublishError)
			return l.Converged && l.PublishedArtifactDigest != "", nil
		})
		if err != nil {
			return fmt.Errorf("%w; last lifecycle %s", err, last)
		}
		fmt.Printf("PASS: satellite %s converged %s after Harbor came back (published %s)\n",
			*satellite, time.Since(start).Round(time.Second), last.PublishedArtifactDigest)
		return poll(ctx, *interval, "/health to be healthy", func() (bool, error) {
			var h health
			if err := g.getJSON(ctx, "/health", &h); err != nil {
				return false, err
			}
			if h.Status == "healthy" {
				fmt.Println("PASS: /health is healthy again")
				return true, nil
			}
			return false, fmt.Errorf("/health is %+v", h)
		})
	}
}
