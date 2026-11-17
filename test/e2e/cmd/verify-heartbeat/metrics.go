package main

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	heartbeatsMetric  = "satellite_heartbeats_total"
	journalAcksMetric = "satellite_journal_acks_total"
)

// heartbeatView is what the satellite's /metrics says about its heartbeats.
type heartbeatView struct {
	ok, refused                 float64
	accepted, rejected, dropped float64
	codes                       map[string]float64
}

// verifySatelliteMetrics waits until the satellite counts two more 200
// heartbeats than at a baseline scrape and no further refusal, with at least
// one accepted journal upload unless -require-journal-ack=false. A refusal moves the baseline; any rejected or
// dropped journal upload fails at once.
func verifySatelliteMetrics(ctx context.Context, o options) error {
	fmt.Printf("Checking the heartbeats in %s's own /metrics...\n", o.satelliteContainer)
	var first *heartbeatView
	var last string
	for i := 1; i <= o.attempts; i++ {
		raw, err := scrape(ctx, o)
		if err != nil {
			last = err.Error()
			fmt.Printf("metrics not readable yet (%d/%d): %v\n", i, o.attempts, err)
			time.Sleep(o.interval)
			continue
		}
		view, err := parseHeartbeatView(raw)
		if err != nil {
			return err
		}
		last = view.String()
		if view.rejected > 0 || view.dropped > 0 {
			return fmt.Errorf("ground control rejected journal uploads: %s", last)
		}
		switch {
		case first == nil:
			first = &view
		case view.refused == first.refused && view.ok >= first.ok+2 && (view.accepted > 0 || !o.requireJournalAck):
			fmt.Printf("PASS: satellite heartbeats answered 200 (%v), journal acks accepted (%v), none refused since %v\n",
				view.ok, view.accepted, first.refused)
			return nil
		case view.refused > first.refused:
			first = &view
		}
		fmt.Printf("Waiting for 200 heartbeats and an accepted journal ack: %s (%d/%d)\n", last, i, o.attempts)
		time.Sleep(o.interval)
	}
	want := "two more 200 heartbeats without a refusal"
	if o.requireJournalAck {
		want += " and an accepted journal ack"
	}
	return fmt.Errorf("satellite metrics never showed %s; last: %s", want, last)
}

func scrape(ctx context.Context, o options) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "exec", o.satelliteContainer, "curl", "-sf", o.metricsURL).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("scrape %s in %s: %w: %s", o.metricsURL, o.satelliteContainer, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func parseHeartbeatView(raw string) (heartbeatView, error) {
	view := heartbeatView{codes: map[string]float64{}}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		name, label, value, ok := sample(line)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return view, fmt.Errorf("parse %q: %w", line, err)
		}
		switch name {
		case heartbeatsMetric:
			view.codes[label] = v
			if label == "200" {
				view.ok = v
			} else {
				view.refused += v
			}
		case journalAcksMetric:
			switch label {
			case "accepted":
				view.accepted = v
			case "rejected":
				view.rejected = v
			case "dropped":
				view.dropped = v
			}
		}
	}
	return view, scanner.Err()
}

// sample splits `name{key="label"} value` with a single label.
func sample(line string) (name, label, value string, ok bool) {
	if strings.HasPrefix(line, "#") {
		return "", "", "", false
	}
	open := strings.IndexByte(line, '{')
	end := strings.LastIndexByte(line, '}')
	if open < 0 || end < open {
		return "", "", "", false
	}
	_, quoted, found := strings.Cut(line[open+1:end], "=")
	if !found {
		return "", "", "", false
	}
	return line[:open], strings.Trim(quoted, `"`), strings.TrimSpace(line[end+1:]), true
}

func (v heartbeatView) String() string {
	return fmt.Sprintf("heartbeats by code %v, journal acks accepted=%v rejected=%v dropped=%v",
		v.codes, v.accepted, v.rejected, v.dropped)
}
