package common

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type fakeResponse struct {
	body   []byte
	status string
	code   int
}

func (f fakeResponse) GetBody() []byte { return f.body }
func (f fakeResponse) Status() string  { return f.status }
func (f fakeResponse) StatusCode() int { return f.code }

func TestPrintResponseHonorsOutputFlag(t *testing.T) {
	response := fakeResponse{body: []byte(`[{"id":"1","name":"edge-01"}]`), status: "200 OK", code: 200}

	table := printWithOutput(t, "table", response)
	require.Contains(t, table, "NAME")
	require.Contains(t, table, "edge-01")

	yamlOut := printWithOutput(t, "yaml", response)
	require.Contains(t, yamlOut, "name: edge-01")

	jsonOut := printWithOutput(t, "json", response)
	require.JSONEq(t, string(response.body), jsonOut)
}

func TestPrintResponseRejectsInvalidOutputFlag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(outputFlagName, "xml", "")
	require.ErrorContains(t, PrintResponse(cmd, fakeResponse{body: []byte(`{}`), code: 200}), "expected json, yaml, or table")
}

func printWithOutput(t *testing.T, format string, response fakeResponse) string {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String(outputFlagName, format, "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, PrintResponse(cmd, response))
	return out.String()
}

func TestParseOutputFormat(t *testing.T) {
	for _, value := range []string{"json", "JSON", " yaml ", "table"} {
		format, err := parseOutputFormat(value)
		require.NoError(t, err)
		require.Equal(t, strings.ToLower(strings.TrimSpace(value)), string(format))
	}

	_, err := parseOutputFormat("xml")
	require.ErrorContains(t, err, "expected json, yaml, or table")
}

// A single list command (satellites) rendered in each format.
func TestRenderListInEveryFormat(t *testing.T) {
	body := []byte(`[
		{"ID":"sat-1","Name":"edge-01","LastSeen":{"Time":"2026-01-02T03:04:05Z","Valid":true},"HeartbeatInterval":{"String":"","Valid":false},"CreatedAt":"2026-01-01T00:00:00Z"},
		{"ID":"sat-2","Name":"edge-02","LastSeen":{"Time":"","Valid":false},"HeartbeatInterval":{"String":"@every 30s","Valid":true},"CreatedAt":"2026-01-01T00:00:00Z"}
	]`)

	jsonOut, err := render(outputJSON, body)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(jsonOut), "JSON output stays the response body")
	require.True(t, strings.HasSuffix(string(jsonOut), "\n"))

	yamlOut, err := render(outputYAML, body)
	require.NoError(t, err)
	require.Contains(t, string(yamlOut), "Name: edge-01")
	require.Contains(t, string(yamlOut), "Valid: true")

	tableOut, err := render(outputTable, body)
	require.NoError(t, err)
	table := string(tableOut)
	lines := strings.Split(strings.TrimRight(table, "\n"), "\n")
	require.Len(t, lines, 3, "a header and one row per item")
	// ID and NAME lead; timestamps trail.
	require.Less(t, strings.Index(lines[0], "ID"), strings.Index(lines[0], "NAME"))
	require.Less(t, strings.Index(lines[0], "NAME"), strings.Index(lines[0], "CREATEDAT"))
	require.Contains(t, table, "edge-01")
	require.Contains(t, table, "edge-02")
	// A valid sql.Null* renders its carried value; an invalid one is blank.
	require.Contains(t, table, "2026-01-02T03:04:05Z")
	require.Contains(t, table, "@every 30s")
}

// A single-object command (one satellite) rendered in each format.
func TestRenderObjectInEveryFormat(t *testing.T) {
	body := []byte(`{"id":"sat-1","username":"operator","role":"admin","groups":["a","b"]}`)

	jsonOut, err := render(outputJSON, body)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(jsonOut))

	yamlOut, err := render(outputYAML, body)
	require.NoError(t, err)
	require.Contains(t, string(yamlOut), "role: admin")

	tableOut, err := render(outputTable, body)
	require.NoError(t, err)
	table := string(tableOut)
	require.Contains(t, table, "FIELD")
	require.Contains(t, table, "VALUE")
	require.Contains(t, table, "role")
	require.Contains(t, table, "admin")
	require.Contains(t, table, "a,b", "a scalar array joins with commas")
}

func TestRenderTableEmptyList(t *testing.T) {
	out, err := render(outputTable, []byte(`[]`))
	require.NoError(t, err)
	require.Equal(t, "No resources found.\n", string(out))
}

func TestRenderNonJSONFallsBackToRawBody(t *testing.T) {
	body := []byte("Created\n")
	for _, format := range []outputFormat{outputJSON, outputYAML, outputTable} {
		out, err := render(format, body)
		require.NoError(t, err)
		require.Equal(t, "Created\n", string(out), "format %s", format)
	}
}

func TestRenderTableKeepsLargeIntegersExact(t *testing.T) {
	out, err := render(outputTable, []byte(`[{"id":9007199254740993,"ratio":0.25,"count":42}]`))
	require.NoError(t, err)
	require.Contains(t, string(out), "9007199254740993")
	require.Contains(t, string(out), "0.25")
	require.Contains(t, string(out), "42")
}

func TestRenderTableUnwrapsOnlySQLNullValues(t *testing.T) {
	body := []byte(`{
		"name": {"String": "edge-01", "Valid": true},
		"seen": {"Time": "2026-01-02T03:04:05Z", "Valid": true},
		"note": {"String": "", "Valid": false},
		"check": {"Name": "store", "Valid": true}
	}`)
	out, err := render(outputTable, body)
	require.NoError(t, err)
	table := string(out)

	require.Contains(t, table, "edge-01")
	require.NotContains(t, table, `"String"`)
	require.Contains(t, table, "2026-01-02T03:04:05Z")
	require.NotContains(t, table, `"Time"`)
	// An object that only happens to carry a Valid field keeps its structure.
	require.Contains(t, table, `{"Name":"store","Valid":true}`)
}
