package state

import (
	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/shared/inventory"
)

// InventoryDigest is internal/shared/inventory.Digest of the cached images,
// so the satellite and Ground Control compute it alike (plan item 4.4). The
// status report sends cached_images only when it changed or Ground Control
// asked with inventory_requested.
func InventoryDigest(images []CachedImage) string {
	items := make([]inventory.Item, 0, len(images))
	for _, img := range images {
		items = append(items, inventory.Item{Reference: img.Reference, SizeBytes: img.SizeBytes})
	}
	return inventory.Digest(items)
}

// inventoryState remembers what Ground Control holds of the inventory.
type inventoryState struct {
	sent      string // digest of the last inventory Ground Control accepted
	requested bool   // Ground Control asked for the list with the last answer
}

// prepare fills the report's digest and drops the list when Ground Control
// already holds it. collected is false when no inventory was collected.
func (s *inventoryState) prepare(report *gcclient.StatusReport, images []CachedImage, collected bool) string {
	if !collected {
		return ""
	}
	digest := InventoryDigest(images)
	report.InventoryDigest = digest
	if digest == s.sent && !s.requested {
		report.CachedImages = nil
	} else if report.CachedImages == nil {
		report.CachedImages = []gcclient.CachedImageReport{}
	}
	return digest
}

// answered records Ground Control's answer to a report that carried digest.
func (s *inventoryState) answered(report gcclient.StatusReport, digest string, requested bool) {
	if digest != "" && report.CachedImages != nil {
		s.sent = digest
	}
	s.requested = requested
}
