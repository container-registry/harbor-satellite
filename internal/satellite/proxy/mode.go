package proxy

import (
	"fmt"
	"strings"
)

// Mode controls whether a local miss can be filled from the upstream registry.
type Mode string

const (
	ModeProxy   Mode = "proxy"
	ModeReplica Mode = "replica"
)

// ParseMode parses a supported proxy operating mode.
func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	if !mode.Valid() {
		return "", fmt.Errorf("invalid proxy mode %q: expected %q or %q", value, ModeProxy, ModeReplica)
	}
	return mode, nil
}

// Valid reports whether mode is supported.
func (mode Mode) Valid() bool {
	return mode == ModeProxy || mode == ModeReplica
}

// AllowsUpstreamPull reports whether a local miss may contact the upstream.
func (mode Mode) AllowsUpstreamPull() bool {
	return mode == ModeProxy
}

func (mode Mode) String() string {
	return string(mode)
}

// Set implements flag.Value.
func (mode *Mode) Set(value string) error {
	parsed, err := ParseMode(value)
	if err != nil {
		return err
	}
	*mode = parsed
	return nil
}

// UnmarshalText supports environment parsing.
func (mode *Mode) UnmarshalText(text []byte) error {
	return mode.Set(string(text))
}
