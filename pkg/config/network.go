package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// NetworkConfig is the optional transfer quota. A disabled block leaves
// replication unchanged.
type NetworkConfig struct {
	Quota NetworkQuotaConfig `json:"quota,omitempty"`
}

// NetworkQuotaConfig caps bytes copied in one window. Limit is a quantity
// such as 5GB. Window is sync, day, or week.
type NetworkQuotaConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Limit   string `json:"limit,omitempty"`
	Window  string `json:"window,omitempty"`
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
	return scaleQuantity(value, prefix)
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

func scaleQuantity(value float64, prefix string) (int64, error) {
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
	// float64(math.MaxInt64) rounds up to 2^63, so a value at that bound does
	// not fit in a positive int64.
	if scaled < 1 || scaled >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("quantity is out of range")
	}
	return int64(scaled), nil
}
