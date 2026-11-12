package edgejournal

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

// MaxStringBytes bounds every string in a journaled payload, and the
// report's last_replication_error. Error texts are unbounded (a registry
// may answer with a whole HTML page), and a heartbeat carries up to 1000
// records under Ground Control's SYNC_MAX_BODY_BYTES (4 MiB). 1 KiB keeps
// the start of an error, which is the informative part.
const MaxStringBytes = 1024

const truncatedSuffix = "...[truncated]"

// Truncate returns s when it fits MaxStringBytes, or its first bytes cut
// at a rune boundary with a marker, MaxStringBytes in total.
func Truncate(s string) string {
	if len(s) <= MaxStringBytes {
		return s
	}
	cut := MaxStringBytes - len(truncatedSuffix)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + truncatedSuffix
}

// BoundPayload returns payload with every string, at any depth, cut with
// Truncate. A payload that holds no longer string, or is not JSON, comes
// back unchanged; numbers keep their exact text.
func BoundPayload(payload json.RawMessage) json.RawMessage {
	if len(payload) <= MaxStringBytes {
		return payload
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return payload
	}
	v, changed := boundValue(v)
	if !changed {
		return payload
	}
	out, err := json.Marshal(v)
	if err != nil {
		return payload
	}
	return out
}

func boundValue(v any) (any, bool) {
	switch t := v.(type) {
	case string:
		cut := Truncate(t)
		return cut, len(cut) != len(t)
	case map[string]any:
		changed := false
		for k, e := range t {
			b, c := boundValue(e)
			t[k], changed = b, changed || c
		}
		return t, changed
	case []any:
		changed := false
		for i, e := range t {
			b, c := boundValue(e)
			t[i], changed = b, changed || c
		}
		return t, changed
	default:
		return v, false
	}
}
