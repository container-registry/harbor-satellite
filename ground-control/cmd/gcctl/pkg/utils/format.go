package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

func PrintFormat(data any, format string) error {
	switch format {
	case "json":
		return printJSON(data)
	case "yaml":
		return printYAML(data)
	default:
		return fmt.Errorf("unsupported output format: %q (use json or yaml)", format)
	}
}

func printJSON(data any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func printYAML(data any) error {
	return yaml.NewEncoder(os.Stdout).Encode(data)
}

func PrintKeyValue(pairs [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	for _, pair := range pairs {
		if len(pair) == 2 {
			fmt.Fprintf(w, "%s:\t%s\n", pair[0], pair[1])
		}
	}
	w.Flush()
}
