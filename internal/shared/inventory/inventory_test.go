package inventory_test

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/inventory"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// The golden values come from shasum -a 256 over the lines Digest documents:
//
//	printf 'harbor.example/library/alpine:3.20\t3145728\nharbor.example/library/busybox:1.36\t1048576\n' | shasum -a 256
//	printf '' | shasum -a 256
func TestDigest_OrderIndependentAndGolden(t *testing.T) {
	pending.Plan(t, "4.4", "the inventory digest is a stub; satellites send their full image list on every heartbeat")
	alpine := inventory.Item{Reference: "harbor.example/library/alpine:3.20", SizeBytes: 3 << 20}
	busybox := inventory.Item{Reference: "harbor.example/library/busybox:1.36", SizeBytes: 1 << 20}
	const golden = "sha256:eaa77371712eb7ca6f654a7c30aa0f7bae15a671527ae76f83ade521ba85e1b1"

	require.Equal(t, golden, inventory.Digest([]inventory.Item{alpine, busybox}))
	require.Equal(t, golden, inventory.Digest([]inventory.Item{busybox, alpine}), "order-independent")
	require.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", inventory.Digest(nil))
	busybox.SizeBytes++
	require.NotEqual(t, golden, inventory.Digest([]inventory.Item{alpine, busybox}), "a size is part of the digest")
}
