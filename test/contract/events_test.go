package contract_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/stretchr/testify/require"
)

const catalogDir = "../../spec/events/v1"

type eventSchema struct {
	ID         string                     `json:"$id"`
	Properties map[string]json.RawMessage `json:"properties"`
}

func readSchema(t *testing.T, kind fleet.EventKind) eventSchema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(catalogDir, string(kind)+".json"))
	require.NoError(t, err, "spec/events/v1 has no schema for %s", kind)
	var s eventSchema
	require.NoError(t, json.Unmarshal(raw, &s), kind)
	return s
}

func TestEventCatalog_CoversEveryEventKind(t *testing.T) {
	var want []string
	for _, kind := range fleet.EventKinds() {
		want = append(want, string(kind)+".json")
	}
	entries, err := os.ReadDir(catalogDir)
	require.NoError(t, err)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(want)
	require.Equal(t, want, got, "one schema per event kind, none left over")
	for _, kind := range fleet.EventKinds() {
		require.Equal(t, webhook.SchemaBaseURL+string(kind)+".json", readSchema(t, kind).ID)
	}
}

var update = flag.Bool("update", false, "rewrite spec/events/v1 from fleet.EventPayloads")

func propertySchema(t *testing.T, typ reflect.Type) map[string]any {
	t.Helper()
	if typ == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	switch typ.Kind() { //nolint:exhaustive // the payloads use only these kinds
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": propertySchema(t, typ.Elem())}
	case reflect.Struct:
		return objectSchema(t, typ)
	}
	t.Fatalf("%s: no JSON Schema for kind %s", typ, typ.Kind())
	return nil
}

// objectSchema flattens untagged embedded structs, as encoding/json does.
// A field without omitempty or omitzero is required.
func objectSchema(t *testing.T, typ reflect.Type) map[string]any {
	t.Helper()
	props := map[string]any{}
	var required []string
	var walk func(reflect.Type)
	walk = func(st reflect.Type) {
		for i := range st.NumField() {
			f := st.Field(i)
			tag, tagged := f.Tag.Lookup("json")
			if f.Anonymous && !tagged {
				walk(f.Type)
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if name == "-" || !f.IsExported() {
				continue
			}
			require.NotEmpty(t, name, "%s.%s has no json name", st, f.Name)
			props[name] = propertySchema(t, f.Type)
			options := strings.Split(opts, ",")
			if !slices.Contains(options, "omitempty") && !slices.Contains(options, "omitzero") {
				required = append(required, name)
			}
		}
	}
	walk(typ)
	s := map[string]any{"type": "object", "additionalProperties": true, "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func generateSchema(t *testing.T, kind fleet.EventKind, payload any) []byte {
	t.Helper()
	s := objectSchema(t, reflect.TypeOf(payload))
	s["$id"] = webhook.SchemaBaseURL + string(kind) + ".json"
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	s["title"] = string(kind)
	s["description"] = fmt.Sprintf("data of the %s event (%s); v1 changes are additive only", webhook.EventType(kind), kind)
	out, err := json.MarshalIndent(s, "", "  ")
	require.NoError(t, err)
	return append(out, '\n')
}

// spec/events/v1 is generated from the payload types:
// go test ./test/contract -run TestEventCatalog_MatchesPayloadTypes -update
func TestEventCatalog_MatchesPayloadTypes(t *testing.T) {
	for kind, payload := range fleet.EventPayloads() {
		want := generateSchema(t, kind, payload)
		file := filepath.Join(catalogDir, string(kind)+".json")
		if *update {
			require.NoError(t, os.WriteFile(file, want, 0o600))
			continue
		}
		got, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, string(want), string(got), "%s: regenerate with -update", kind)
	}
}
