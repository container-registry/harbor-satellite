// Package inventory is the cached-image inventory a satellite reports and
// Ground Control stores (plan item 4.4). Both sides compute its digest
// alike, so a satellite sends the list only when the digest changed or
// Ground Control asks for it.
package inventory

type Item struct {
	Reference string
	SizeBytes int64
}

// Digest is "sha256:" + hex(sha256 of the items sorted by Reference, one
// "<reference>\t<size>\n" line each), so the order of items does not
// matter. An empty inventory has the digest of no lines.
func Digest(items []Item) string {
	return ""
}
