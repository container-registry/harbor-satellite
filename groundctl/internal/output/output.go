package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/container-registry/harbor-satellite/groundctl/internal/client"
)

// Format controls the output format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

// PrintSatellites prints a list of satellites in the given format.
func PrintSatellites(satellites []client.Satellite, format Format) {
	switch format {
	case FormatJSON:
		printJSON(satellites)
	default:
		printSatelliteTable(satellites, os.Stdout)
	}
}

// PrintSatellite prints a single satellite in the given format.
func PrintSatellite(sat *client.Satellite, format Format) {
	switch format {
	case FormatJSON:
		printJSON(sat)
	default:
		printSatelliteTable([]client.Satellite{*sat}, os.Stdout)
	}
}

// PrintSatelliteStatus prints satellite status in the given format.
func PrintSatelliteStatus(name string, status *client.SatelliteStatus, format Format) {
	switch format {
	case FormatJSON:
		printJSON(status)
	default:
		printStatusTable(name, status, os.Stdout)
	}
}

// PrintCachedImages prints the cached images of a satellite.
func PrintCachedImages(name string, images []client.CachedImage, format Format) {
	switch format {
	case FormatJSON:
		printJSON(images)
	default:
		printImagesTable(name, images, os.Stdout)
	}
}

// PrintGroups prints a list of groups in the given format.
func PrintGroups(groups []client.Group, format Format) {
	switch format {
	case FormatJSON:
		printJSON(groups)
	default:
		printGroupTable(groups, os.Stdout)
	}
}

// PrintConfigs prints a list of configs in the given format.
func PrintConfigs(configs []client.Config, format Format) {
	switch format {
	case FormatJSON:
		printJSON(configs)
	default:
		printConfigTable(configs, os.Stdout)
	}
}

// PrintApplyResult prints the result of a groundctl apply operation.
func PrintApplyResult(created, deleted, updated, unchanged []string) {
	for _, name := range created {
		fmt.Printf("  \033[32m+\033[0m satellite/%-30s created\n", name)
	}
	for _, name := range deleted {
		fmt.Printf("  \033[31m-\033[0m satellite/%-30s deleted\n", name)
	}
	for _, name := range updated {
		fmt.Printf("  \033[33m~\033[0m satellite/%-30s updated\n", name)
	}
	for _, name := range unchanged {
		fmt.Printf("    satellite/%-30s unchanged\n", name)
	}

	total := len(created) + len(deleted) + len(updated)
	if total == 0 {
		fmt.Println("\nNo changes. Fleet is already in sync.")
	} else {
		fmt.Printf("\nApplied: %d created, %d deleted, %d updated, %d unchanged\n",
			len(created), len(deleted), len(updated), len(unchanged))
	}
}

func printSatelliteTable(satellites []client.Satellite, w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATUS\tLAST SEEN\tHEARTBEAT\tCREATED")
	for _, s := range satellites {
		lastSeen := "<never>"
		if s.LastSeen != nil {
			lastSeen = formatAge(*s.LastSeen)
		}
		heartbeat := "-"
		if s.HeartbeatInterval != nil {
			heartbeat = *s.HeartbeatInterval
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			s.Name,
			statusEmoji(s.LastSeen),
			lastSeen,
			heartbeat,
			formatAge(s.CreatedAt),
		)
	}
	tw.Flush()
}

func printStatusTable(name string, s *client.SatelliteStatus, w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Satellite:\t%s\n", name)
	fmt.Fprintf(tw, "Activity:\t%s\n", s.Activity)
	fmt.Fprintf(tw, "State Digest:\t%s\n", truncate(s.LatestStateDigest, 20))
	fmt.Fprintf(tw, "Config Digest:\t%s\n", truncate(s.LatestConfigDigest, 20))
	fmt.Fprintf(tw, "CPU:\t%s%%\n", s.CPUPercent)
	fmt.Fprintf(tw, "Memory:\t%s\n", formatBytes(s.MemoryUsedBytes))
	fmt.Fprintf(tw, "Storage:\t%s\n", formatBytes(s.StorageUsedBytes))
	fmt.Fprintf(tw, "Images Cached:\t%d\n", s.ImageCount)
	fmt.Fprintf(tw, "Last Sync:\t%dms\n", s.LastSyncDurationMs)
	fmt.Fprintf(tw, "Reported At:\t%s\n", formatAge(s.ReportedAt))
	tw.Flush()
}

func printImagesTable(name string, images []client.CachedImage, w io.Writer) {
	fmt.Fprintf(w, "Cached images on %s (%d total):\n\n", name, len(images))
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "REFERENCE\tSIZE")
	for _, img := range images {
		fmt.Fprintf(tw, "%s\t%s\n", img.Reference, formatBytes(img.SizeBytes))
	}
	tw.Flush()
}

func printGroupTable(groups []client.Group, w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tCREATED")
	for _, g := range groups {
		fmt.Fprintf(tw, "%s\t%s\n", g.GroupName, formatAge(g.CreatedAt))
	}
	tw.Flush()
}

func printConfigTable(configs []client.Config, w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tCREATED\tUPDATED")
	for _, c := range configs {
		fmt.Fprintf(tw, "%s\t%s\t%s\n",
			c.Name,
			formatAge(c.CreatedAt),
			formatAge(c.UpdatedAt),
		)
	}
	tw.Flush()
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func statusEmoji(lastSeen *time.Time) string {
	if lastSeen == nil {
		return "Unknown"
	}
	if time.Since(*lastSeen) < 2*time.Minute {
		return "Active"
	}
	if time.Since(*lastSeen) < time.Hour {
		return "Idle"
	}
	return "Stale"
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
