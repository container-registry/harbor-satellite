// Package inventory is the cached-image inventory a satellite reports and
// Ground Control stores (plan item 4.4). Both sides compute its digest
// alike, so a satellite sends the list only when the digest changed or
// Ground Control asks for it.
package inventory

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
)

type Item struct {
	Reference string
	SizeBytes int64
}

// Digest is "sha256:" + hex(sha256 of the items sorted by Reference, one
// "<reference>\t<size>\n" line each), so the order of items does not
// matter. An empty inventory has the digest of no lines.
func Digest(items []Item) string {
	sorted := slices.Clone(items)
	slices.SortFunc(sorted, func(a, b Item) int {
		return cmp.Or(cmp.Compare(a.Reference, b.Reference), cmp.Compare(a.SizeBytes, b.SizeBytes))
	})
	h := sha256.New()
	for _, it := range sorted {
		h.Write([]byte(it.Reference))
		h.Write([]byte{'\t'})
		h.Write(strconv.AppendInt(nil, it.SizeBytes, 10))
		h.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
