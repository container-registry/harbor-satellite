package logger

import (
	"encoding/json"
	"reflect"
	"strings"
)

// RedactedValue is substituted for sensitive configuration values in audit
// events.
const RedactedValue = "[REDACTED]"

// RedactConfig returns a decoded, deep-copied configuration with sensitive
// values removed. Invalid JSON is represented by a fixed placeholder so the
// original payload can never leak into an audit event.
func RedactConfig(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "[unparseable config]"
	}
	return redactConfigValue(decoded)
}

// DiffConfig returns a flattened, redacted map of configuration changes.
func DiffConfig(oldRaw, newRaw []byte) map[string]any {
	var oldValue, newValue any
	if err := json.Unmarshal(oldRaw, &oldValue); err != nil {
		oldValue = nil
	}
	if err := json.Unmarshal(newRaw, &newValue); err != nil {
		newValue = nil
	}

	diff := map[string]any{}
	diffConfigValues(diff, "", oldValue, newValue)
	if len(diff) == 0 {
		return nil
	}
	return diff
}

func isSensitiveConfigKey(key string) bool {
	key = strings.ToLower(key)
	for _, fragment := range []string{
		"password", "passwd", "secret", "token", "credential", "apikey", "api_key", "access_key", "private_key",
	} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func redactConfigValue(value any) any {
	switch typedValue := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typedValue))
		for key, child := range typedValue {
			if isSensitiveConfigKey(key) {
				redacted[key] = RedactedValue
			} else {
				redacted[key] = redactConfigValue(child)
			}
		}
		return redacted
	case []any:
		redacted := make([]any, len(typedValue))
		for index, child := range typedValue {
			redacted[index] = redactConfigValue(child)
		}
		return redacted
	default:
		return value
	}
}

func diffConfigValues(diff map[string]any, prefix string, oldValue, newValue any) {
	oldMap, oldIsMap := oldValue.(map[string]any)
	newMap, newIsMap := newValue.(map[string]any)
	if oldIsMap && newIsMap {
		diffConfigMaps(diff, prefix, oldMap, newMap)
		return
	}
	if !reflect.DeepEqual(oldValue, newValue) {
		key := lastConfigKey(prefix)
		diff[prefix] = map[string]any{
			"from": redactedConfigLeaf(key, oldValue),
			"to":   redactedConfigLeaf(key, newValue),
		}
	}
}

func diffConfigMaps(diff map[string]any, prefix string, oldMap, newMap map[string]any) {
	for key, oldValue := range oldMap {
		path := childConfigPath(prefix, key)
		if newValue, exists := newMap[key]; exists {
			diffConfigValues(diff, path, oldValue, newValue)
		} else if oldValue != nil {
			diff[path] = map[string]any{"from": redactedConfigLeaf(key, oldValue)}
		}
	}
	for key, newValue := range newMap {
		if _, exists := oldMap[key]; exists || newValue == nil {
			continue
		}
		diff[childConfigPath(prefix, key)] = map[string]any{"to": redactedConfigLeaf(key, newValue)}
	}
}

func childConfigPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func lastConfigKey(path string) string {
	if separator := strings.LastIndexByte(path, '.'); separator >= 0 {
		return path[separator+1:]
	}
	return path
}

func redactedConfigLeaf(key string, value any) any {
	if isSensitiveConfigKey(key) {
		return RedactedValue
	}
	return redactConfigValue(value)
}
