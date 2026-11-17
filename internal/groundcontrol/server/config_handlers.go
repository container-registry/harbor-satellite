package server

import (
	"encoding/json"
	"reflect"
	"strings"
)

// auditRedacted is the placeholder substituted for sensitive config values
// before a config is recorded in the audit log.
const auditRedacted = "[REDACTED]"

// isSensitiveConfigKey reports whether a config key names a secret that must not
// be written to the audit log. Matched case-insensitively as a substring so
// variants (e.g. "password", "secret_key") are all caught.
func isSensitiveConfigKey(key string) bool {
	k := strings.ToLower(key)
	for _, frag := range []string{"password", "passwd", "secret", "token", "credential", "apikey", "api_key", "access_key", "private_key"} {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}

// redactConfigValue returns a deep copy of a decoded JSON value with the values
// of sensitive keys replaced. It recurses through objects and arrays.
func redactConfigValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isSensitiveConfigKey(k) {
				out[k] = auditRedacted
			} else {
				out[k] = redactConfigValue(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactConfigValue(val)
		}
		return out
	default:
		return v
	}
}

// redactConfigForAudit decodes raw config JSON and returns a secret-free copy
// safe to embed in an audit event's details.from / details.to. On empty input
// it returns nil; if the JSON cannot be decoded it returns a placeholder rather
// than the raw bytes, so secrets can never leak through a parse failure.
func redactConfigForAudit(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "[unparseable config]"
	}
	return redactConfigValue(decoded)
}

// childPath joins a dotted config path. The root prefix is empty.
func childPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// lastKey returns the final segment of a dotted path, used to decide whether a
// changed value is sensitive.
func lastKey(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// redactedLeaf returns a changed value safe for the audit log: sensitive keys
// collapse to the placeholder, other values keep their (recursively redacted)
// content so the change is visible.
func redactedLeaf(key string, v any) any {
	if isSensitiveConfigKey(key) {
		return auditRedacted
	}
	return redactConfigValue(v)
}

// diffConfigValues records, into out, every leaf path under prefix whose value
// differs between old and new. Nested objects are compared key-by-key; other
// values are compared whole. A changed leaf is recorded as {from, to} (either
// side omitted when the key was added or removed), with values redacted.
func diffConfigValues(out map[string]any, prefix string, oldV, newV any) {
	oldMap, oldIsMap := oldV.(map[string]any)
	newMap, newIsMap := newV.(map[string]any)
	if oldIsMap && newIsMap {
		diffConfigMaps(out, prefix, oldMap, newMap)
		return
	}
	if !reflect.DeepEqual(oldV, newV) {
		k := lastKey(prefix)
		out[prefix] = map[string]any{"from": redactedLeaf(k, oldV), "to": redactedLeaf(k, newV)}
	}
}

// diffConfigMaps recurses over the union of keys in two objects.
func diffConfigMaps(out map[string]any, prefix string, oldMap, newMap map[string]any) {
	for k, ov := range oldMap {
		path := childPath(prefix, k)
		if nv, ok := newMap[k]; ok {
			diffConfigValues(out, path, ov, nv)
		} else if ov != nil {
			// A key dropping from null to absent is not a real change.
			out[path] = map[string]any{"from": redactedLeaf(k, ov)}
		}
	}
	for k, nv := range newMap {
		if _, ok := oldMap[k]; ok {
			continue
		}
		if nv != nil {
			out[childPath(prefix, k)] = map[string]any{"to": redactedLeaf(k, nv)}
		}
	}
}

// diffConfigForAudit returns a flat map of changed config paths -> {from, to}
// (redacted), computed from the raw old and new config JSON. It returns nil when
// nothing changed. Because the comparison runs on the raw values, a rotated
// secret is reported as a changed path even though its value stays redacted.
func diffConfigForAudit(oldRaw, newRaw []byte) map[string]any {
	var oldV, newV any
	if err := json.Unmarshal(oldRaw, &oldV); err != nil {
		oldV = nil
	}
	if err := json.Unmarshal(newRaw, &newV); err != nil {
		newV = nil
	}
	out := map[string]any{}
	diffConfigValues(out, "", oldV, newV)
	if len(out) == 0 {
		return nil
	}
	return out
}
