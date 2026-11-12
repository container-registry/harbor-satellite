package config

import (
	"fmt"
	"strings"
)

// ProxyMode controls whether a local miss can be filled from the upstream registry.
type ProxyMode string

const (
	ProxyModeProxy   ProxyMode = "proxy"
	ProxyModeReplica ProxyMode = "replica"
)

// ParseProxyMode parses a supported proxy operating mode.
func ParseProxyMode(value string) (ProxyMode, error) {
	mode := ProxyMode(strings.ToLower(strings.TrimSpace(value)))
	if !mode.Valid() {
		return "", fmt.Errorf("invalid proxy mode %q: expected %q or %q", value, ProxyModeProxy, ProxyModeReplica)
	}
	return mode, nil
}

// Valid reports whether mode is supported.
func (mode ProxyMode) Valid() bool {
	return mode == ProxyModeProxy || mode == ProxyModeReplica
}

// AllowsUpstreamPull reports whether a local miss may contact the upstream.
func (mode ProxyMode) AllowsUpstreamPull() bool {
	return mode == ProxyModeProxy
}

func (mode ProxyMode) String() string {
	return string(mode)
}

// Set implements flag.Value.
func (mode *ProxyMode) Set(value string) error {
	parsed, err := ParseProxyMode(value)
	if err != nil {
		return err
	}
	*mode = parsed
	return nil
}

// UnmarshalText supports environment parsing.
func (mode *ProxyMode) UnmarshalText(text []byte) error {
	return mode.Set(string(text))
}
