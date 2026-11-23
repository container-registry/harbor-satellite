package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// outputFormat selects how PrintResponse renders a successful response body.
type outputFormat string

const (
	// outputJSON prints indented JSON and is the default, so existing scripts keep working.
	outputJSON outputFormat = "json"
	// outputYAML prints the response body as YAML.
	outputYAML outputFormat = "yaml"
	// outputTable prints a human-readable table.
	outputTable outputFormat = "table"

	outputFlagName = "output"
)

// parseOutputFormat validates an --output value.
func parseOutputFormat(value string) (outputFormat, error) {
	switch outputFormat(strings.ToLower(strings.TrimSpace(value))) {
	case outputJSON:
		return outputJSON, nil
	case outputYAML:
		return outputYAML, nil
	case outputTable:
		return outputTable, nil
	default:
		return "", fmt.Errorf("invalid --output %q: expected json, yaml, or table", value)
	}
}

// resolveOutputFormat reads the --output flag for the command. An absent flag
// (for example in a directly constructed test command) falls back to JSON.
func resolveOutputFormat(command *cobra.Command) (outputFormat, error) {
	flag := command.Flags().Lookup(outputFlagName)
	if flag == nil {
		return outputJSON, nil
	}
	return parseOutputFormat(flag.Value.String())
}

// render turns a JSON response body into the requested output format. The result
// always ends with a trailing newline. A body that is not valid JSON is written
// through unchanged for every format, so non-JSON responses keep working.
func render(format outputFormat, body []byte) ([]byte, error) {
	switch format {
	case outputYAML:
		return renderYAML(body), nil
	case outputTable:
		return renderTable(body), nil
	case outputJSON:
		return renderJSON(body), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

func renderJSON(body []byte) []byte {
	var formatted bytes.Buffer
	if json.Indent(&formatted, body, "", "  ") == nil {
		return append(formatted.Bytes(), '\n')
	}
	return withTrailingNewline(body)
}

func renderYAML(body []byte) []byte {
	out, err := yaml.JSONToYAML(body)
	if err != nil {
		return withTrailingNewline(body)
	}
	return out
}

func renderTable(body []byte) []byte {
	data, err := decodeJSON(body)
	if err != nil {
		return withTrailingNewline(body)
	}
	switch value := data.(type) {
	case []any:
		return tableFromSlice(value)
	case map[string]any:
		return tableFromObject(value)
	default:
		return []byte(formatCell(value) + "\n")
	}
}

// decodeJSON decodes one JSON value and keeps numbers as json.Number, so a
// large integer is printed exactly as the server sent it.
func decodeJSON(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var data any
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return data, nil
}

func tableFromSlice(items []any) []byte {
	if len(items) == 0 {
		return []byte("No resources found.\n")
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			// A list of scalars renders as a single VALUE column.
			return scalarList(items)
		}
		rows = append(rows, row)
	}

	columns := columnsOf(rows)
	var buffer bytes.Buffer
	writer := newTabWriter(&buffer)
	fmt.Fprintln(writer, strings.Join(headers(columns), "\t"))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for index, column := range columns {
			cells[index] = formatCell(row[column])
		}
		fmt.Fprintln(writer, strings.Join(cells, "\t"))
	}
	_ = writer.Flush()
	return buffer.Bytes()
}

func scalarList(items []any) []byte {
	var buffer bytes.Buffer
	writer := newTabWriter(&buffer)
	fmt.Fprintln(writer, "VALUE")
	for _, item := range items {
		fmt.Fprintln(writer, formatCell(item))
	}
	_ = writer.Flush()
	return buffer.Bytes()
}

func tableFromObject(object map[string]any) []byte {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sortColumns(keys)

	var buffer bytes.Buffer
	writer := newTabWriter(&buffer)
	fmt.Fprintln(writer, "FIELD\tVALUE")
	for _, key := range keys {
		fmt.Fprintf(writer, "%s\t%s\n", key, formatCell(object[key]))
	}
	_ = writer.Flush()
	return buffer.Bytes()
}

func newTabWriter(buffer *bytes.Buffer) *tabwriter.Writer {
	return tabwriter.NewWriter(buffer, 0, 0, 3, ' ', 0)
}

func headers(columns []string) []string {
	out := make([]string, len(columns))
	for index, column := range columns {
		out[index] = strings.ToUpper(column)
	}
	return out
}

// columnsOf collects the union of keys across rows and orders them so that
// identifying columns lead and timestamps trail.
func columnsOf(rows []map[string]any) []string {
	seen := make(map[string]bool)
	var columns []string
	for _, row := range rows {
		for key := range row {
			if !seen[key] {
				seen[key] = true
				columns = append(columns, key)
			}
		}
	}
	sortColumns(columns)
	return columns
}

func sortColumns(columns []string) {
	sort.SliceStable(columns, func(i, j int) bool {
		ri, rj := columnRank(columns[i]), columnRank(columns[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(columns[i]) < strings.ToLower(columns[j])
	})
}

func columnRank(name string) int {
	switch strings.ToLower(name) {
	case "id":
		return 0
	case "name", "groupname", "configname", "username":
		return 1
	case "role", "state", "status":
		return 2
	case "createdat", "created_at":
		return 8
	case "updatedat", "updated_at":
		return 9
	default:
		return 5
	}
}

func formatCell(value any) string {
	return sanitizeCell(formatValue(value))
}

func formatValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, formatValue(item))
		}
		return strings.Join(parts, ",")
	case map[string]any:
		if text, ok := nullValue(typed); ok {
			return text
		}
		return compactJSON(typed)
	default:
		return compactJSON(typed)
	}
}

// sqlNullFields lists the value field of every database/sql Null* type.
func sqlNullFields() []string {
	return []string{"String", "Int64", "Int32", "Int16", "Byte", "Float64", "Bool", "Time"}
}

// nullValue renders a database/sql Null* value ({"Valid": bool, "<T>": value})
// as the carried value, or empty when it is not valid. Any other object that
// has a Valid field is left alone.
func nullValue(object map[string]any) (string, bool) {
	valid, ok := object["Valid"].(bool)
	if !ok || len(object) != 2 {
		return "", false
	}
	for _, field := range sqlNullFields() {
		value, exists := object[field]
		if !exists {
			continue
		}
		if !valid {
			return "", true
		}
		return formatValue(value), true
	}
	return "", false
}

func compactJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

func sanitizeCell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\t", " ")
}

func withTrailingNewline(body []byte) []byte {
	trimmed := bytes.TrimRight(body, "\n")
	return append(trimmed, '\n')
}
