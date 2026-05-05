package output

import (
	"fmt"
	"os"
	"text/tabwriter"
)

// PrintTable prints a formatted table with headers and rows to stdout.
func PrintTable(headers []string, rows [][]string) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(writer, "\t")
		}
		fmt.Fprint(writer, h)
	}
	fmt.Fprintln(writer)

	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(writer, "\t")
			}
			fmt.Fprint(writer, cell)
		}
		fmt.Fprintln(writer)
	}

	writer.Flush()
}

// PrintKeyValue prints key-value pairs as a two-column aligned output without headers.
func PrintKeyValue(rows [][]string) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(writer, "\t")
			}
			fmt.Fprint(writer, cell)
		}
		fmt.Fprintln(writer)
	}

	writer.Flush()
}
