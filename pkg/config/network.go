package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// NetworkConfig is the optional speed cap and transfer quota. A disabled
// block leaves replication unchanged.
type NetworkConfig struct {
	Speed NetworkSpeedConfig `json:"speed,omitempty"`
	Quota NetworkQuotaConfig `json:"quota,omitempty"`
}

// NetworkSpeedConfig caps the Harbor download rate. Limit is a quantity such
// as 10Mbps.
type NetworkSpeedConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Limit   string `json:"limit,omitempty"`
}

// NetworkQuotaConfig caps bytes copied in one window. Limit is a quantity
// such as 5GB. Window is sync, day, or week.
type NetworkQuotaConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Limit   string `json:"limit,omitempty"`
	Window  string `json:"window,omitempty"`
}

// ParseByteRate parses a speed such as 10Mbps into bytes per second.
// Named bit rates use decimal units. Named byte rates accept decimal (MB/s)
// and binary (MiB/s) units.
func ParseByteRate(raw string) (int64, error) {
	value, unit, err := splitQuantity(raw)
	if err != nil {
		return 0, fmt.Errorf("parse speed %q: %w", raw, err)
	}
	perSecond, bits, prefix, err := speedUnit(unit)
	if err != nil {
		return 0, fmt.Errorf("parse speed %q: %w", raw, err)
	}
	if !perSecond {
		return 0, fmt.Errorf("speed %q must be a rate such as 10Mbps", raw)
	}
	return scaleQuantity(value, prefix, bits)
}

// ParseByteSize parses a size such as 5GB into bytes. A bare number is bytes.
func ParseByteSize(raw string) (int64, error) {
	value, unit, err := splitQuantity(raw)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", raw, err)
	}
	prefix, err := sizeUnit(unit)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", raw, err)
	}
	return scaleQuantity(value, prefix, false)
}

func splitQuantity(raw string) (float64, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", fmt.Errorf("empty quantity")
	}
	index := 0
	for index < len(raw) && (unicode.IsDigit(rune(raw[index])) || raw[index] == '.') {
		index++
	}
	if index == 0 {
		return 0, "", fmt.Errorf("missing number")
	}
	value, err := strconv.ParseFloat(raw[:index], 64)
	if err != nil {
		return 0, "", err
	}
	unit := strings.TrimSpace(raw[index:])
	return value, unit, nil
}

func speedUnit(unit string) (bool, bool, string, error) {
	switch strings.ToLower(unit) {
	case "bps":
		return true, true, "", nil
	case "kbps":
		return true, true, "k", nil
	case "mbps":
		return true, true, "m", nil
	case "gbps":
		return true, true, "g", nil
	case "b/s":
		return true, false, "", nil
	case "kb/s":
		return true, false, "k", nil
	case "mb/s":
		return true, false, "m", nil
	case "gb/s":
		return true, false, "g", nil
	case "kib/s":
		return true, false, "ki", nil
	case "mib/s":
		return true, false, "mi", nil
	case "gib/s":
		return true, false, "gi", nil
	default:
		return false, false, "", fmt.Errorf("unknown speed unit %q", unit)
	}
}

func sizeUnit(unit string) (string, error) {
	switch strings.ToLower(unit) {
	case "", "b":
		return "", nil
	case "kb":
		return "k", nil
	case "mb":
		return "m", nil
	case "gb":
		return "g", nil
	case "tb":
		return "t", nil
	case "kib":
		return "ki", nil
	case "mib":
		return "mi", nil
	case "gib":
		return "gi", nil
	case "tib":
		return "ti", nil
	default:
		return "", fmt.Errorf("unknown size unit %q", unit)
	}
}

func scaleQuantity(value float64, prefix string, bits bool) (int64, error) {
	if value <= 0 {
		return 0, fmt.Errorf("quantity must be positive")
	}
	var multiplier float64
	switch prefix {
	case "":
		multiplier = 1
	case "k":
		multiplier = 1_000
	case "m":
		multiplier = 1_000_000
	case "g":
		multiplier = 1_000_000_000
	case "t":
		multiplier = 1_000_000_000_000
	case "ki":
		multiplier = 1024
	case "mi":
		multiplier = 1024 * 1024
	case "gi":
		multiplier = 1024 * 1024 * 1024
	case "ti":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown prefix %q", prefix)
	}
	scaled := value * multiplier
	if bits {
		scaled /= 8
	}
	if scaled < 1 || scaled > float64(math.MaxInt64) {
		return 0, fmt.Errorf("quantity is out of range")
	}
	return int64(scaled), nil
}
