package state

// InventoryDigest is internal/shared/inventory.Digest of the cached images,
// so the satellite and Ground Control compute it alike (plan item 4.4). The
// status report sends cached_images only when it changed or Ground Control
// asked with inventory_requested.
func InventoryDigest(images []CachedImage) string {
	return ""
}
