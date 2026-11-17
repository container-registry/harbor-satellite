package store

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

const testBundleType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// signedSource is a registry holding one image, its cosign tag-scheme
// signature and one OCI 1.1 referrer.
type signedSource struct {
	store     *RegistryStore
	repo      *orasremote.Repository
	image     ocispec.Descriptor
	signature ocispec.Descriptor
	referrer  ocispec.Descriptor
}

func newSignedSource(t *testing.T) signedSource {
	t.Helper()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "http://")
	repo, err := orasremote.NewRepository(address + "/team/app")
	require.NoError(t, err)
	repo.PlainHTTP = true
	source, err := NewRegistryStore(RegistryOptions{Endpoint: address, PlainHTTP: true})
	require.NoError(t, err)

	image := pushTestManifest(t, repo, "latest", []byte("image-layer"), nil, "")
	signature := pushTestManifest(t, repo, supplyChainTag(image.Digest, ".sig"), []byte("cosign-signature"), nil, "")
	referrer := pushTestManifest(t, repo, "", []byte("sigstore-bundle"), &image, testBundleType)
	return signedSource{store: source, repo: repo, image: image, signature: signature, referrer: referrer}
}

// pushTestManifest pushes a one-layer manifest. A subject makes it a referrer;
// an empty tag pushes it by digest.
func pushTestManifest(t *testing.T, repo *orasremote.Repository, tag string, layer []byte, subject *ocispec.Descriptor, artifactType string) ocispec.Descriptor {
	t.Helper()
	ctx := context.Background()
	layerDesc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", layer)
	require.NoError(t, pushIfMissing(ctx, repo, layerDesc, layer))
	config := ocispec.DescriptorEmptyJSON
	require.NoError(t, pushIfMissing(ctx, repo, config, config.Data))
	payload, err := json.Marshal(ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       config,
		Layers:       []ocispec.Descriptor{layerDesc},
		Subject:      subject,
	})
	require.NoError(t, err)
	desc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, payload)
	desc.ArtifactType = artifactType
	if tag == "" {
		require.NoError(t, repo.Push(ctx, desc, bytes.NewReader(payload)))
	} else {
		require.NoError(t, repo.PushReference(ctx, desc, bytes.NewReader(payload), tag))
	}
	return desc
}

func pushIfMissing(ctx context.Context, repo *orasremote.Repository, desc ocispec.Descriptor, payload []byte) error {
	err := repo.Push(ctx, desc, bytes.NewReader(payload))
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

func pushTestIndex(t *testing.T, repo *orasremote.Repository, tag string, manifests ...ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()
	payload, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	})
	require.NoError(t, err)
	desc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageIndex, payload)
	require.NoError(t, repo.PushReference(context.Background(), desc, bytes.NewReader(payload), tag))
	return desc
}

func TestOCIStoreCopiesCosignSignatureAndReferrers(t *testing.T) {
	ctx := context.Background()
	src := newSignedSource(t)
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	artifact := Artifact{Name: "team/app", Tag: "latest", Digest: src.image.Digest.String()}

	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{artifact}))

	signature, err := local.Pull(ctx, Artifact{Name: "team/app", Tag: supplyChainTag(src.image.Digest, ".sig")}, PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, src.signature.Digest, signature.Digest)

	referrers, err := local.Referrers(ctx, Artifact{Name: "team/app"}, src.image.Digest, "")
	require.NoError(t, err)
	require.Len(t, referrers, 1)
	require.Equal(t, src.referrer.Digest, referrers[0].Digest)
	require.Equal(t, testBundleType, referrers[0].ArtifactType)

	filtered, err := local.Referrers(ctx, Artifact{Name: "team/app"}, src.image.Digest, "application/vnd.example.other")
	require.NoError(t, err)
	require.Empty(t, filtered)

	// A referrer is untagged. A pull by digest must still name it a manifest.
	pulled, err := local.Pull(ctx, Artifact{Name: "team/app", Tag: src.referrer.Digest.String()}, PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, ocispec.MediaTypeImageManifest, pulled.MediaType)

	_, err = local.Pull(ctx, Artifact{Name: "other/app", Tag: src.referrer.Digest.String()}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)

	none, err := local.Referrers(ctx, Artifact{Name: "team/app"}, src.signature.Digest, "")
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestOCIStoreCopiesSignaturesOfIndexChildren(t *testing.T) {
	ctx := context.Background()
	src := newSignedSource(t)
	child := pushTestManifest(t, src.repo, "", []byte("arm64-layer"), nil, "")
	child.Platform = &ocispec.Platform{OS: "linux", Architecture: "arm64"}
	childSignature := pushTestManifest(t, src.repo, supplyChainTag(child.Digest, ".sig"), []byte("child-signature"), nil, "")
	childAttestation := pushTestManifest(t, src.repo, supplyChainTag(child.Digest, ".att"), []byte("child-attestation"), nil, "")
	index := pushTestIndex(t, src.repo, "multi", child)
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{{Name: "team/app", Tag: "multi", Digest: index.Digest.String()}}))

	for tag, want := range map[string]digest.Digest{
		supplyChainTag(child.Digest, ".sig"): childSignature.Digest,
		supplyChainTag(child.Digest, ".att"): childAttestation.Digest,
	} {
		got, err := local.Pull(ctx, Artifact{Name: "team/app", Tag: tag}, PullResourceManifest)
		require.NoError(t, err, tag)
		require.Equal(t, want, got.Digest, tag)
	}
	_, err = local.Pull(ctx, Artifact{Name: "team/app", Tag: supplyChainTag(index.Digest, ".sig")}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
}

func TestOCIStoreSkipsSignaturesOfUnlistedDigest(t *testing.T) {
	ctx := context.Background()
	src := newSignedSource(t)
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	artifact := Artifact{Name: "team/app", Tag: "latest", Digest: src.image.Digest.String()}
	remoteTarget, err := src.store.targetFor(artifact)
	require.NoError(t, err)
	staged, err := local.stage(ctx, src.store, remoteTarget, artifact)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, staged.cleanup()) })
	require.Len(t, staged.related, 2, "stage holds the cosign tag and the referrers index")

	rejected := artifact
	rejected.Digest = digest.FromString("some other root").String()
	func() {
		local.mu.Lock()
		defer local.mu.Unlock()
		err = local.publish(ctx, staged, rejected)
	}()
	require.ErrorContains(t, err, "does not match group list digest")

	_, err = local.Pull(ctx, Artifact{Name: "team/app", Tag: supplyChainTag(src.image.Digest, ".sig")}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
	referrers, err := local.Referrers(ctx, Artifact{Name: "team/app"}, src.image.Digest, "")
	require.NoError(t, err)
	require.Empty(t, referrers)
}

func TestOCIStoreDeleteDropsSignaturesWithTheirImage(t *testing.T) {
	ctx := context.Background()
	src := newSignedSource(t)
	require.NoError(t, src.repo.Tag(ctx, src.image, "stable"))
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	latest := Artifact{Name: "team/app", Tag: "latest", Digest: src.image.Digest.String()}
	stable := Artifact{Name: "team/app", Tag: "stable", Digest: src.image.Digest.String()}
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latest, stable}))
	sigTag := Artifact{Name: "team/app", Tag: supplyChainTag(src.image.Digest, ".sig")}

	// Another tag still names the image, so its signatures stay.
	require.NoError(t, local.Delete(ctx, []Artifact{latest}))
	_, err = local.Pull(ctx, sigTag, PullResourceManifest)
	require.NoError(t, err)
	referrers, err := local.Referrers(ctx, Artifact{Name: "team/app"}, src.image.Digest, "")
	require.NoError(t, err)
	require.Len(t, referrers, 1)

	require.NoError(t, local.Delete(ctx, []Artifact{stable}))
	_, err = local.Pull(ctx, sigTag, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
	referrers, err = local.Referrers(ctx, Artifact{Name: "team/app"}, src.image.Digest, "")
	require.NoError(t, err)
	require.Empty(t, referrers)
	exists, err := local.store.Exists(ctx, src.referrer)
	require.NoError(t, err)
	require.False(t, exists, "GC keeps an orphaned referrer")
}

func TestRegistryStoreCopiesCosignSignatureAndReferrers(t *testing.T) {
	ctx := context.Background()
	src := newSignedSource(t)
	destination := plainRegistry(t)
	artifact := Artifact{Name: "team/app", Tag: "latest", Digest: src.image.Digest.String()}

	require.NoError(t, destination.Replicate(ctx, src.store, []Artifact{artifact}))

	signature, err := destination.Pull(ctx, Artifact{Name: "team/app", Tag: supplyChainTag(src.image.Digest, ".sig")}, PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, src.signature.Digest, signature.Digest)
	referrers, err := destination.Referrers(ctx, Artifact{Name: "team/app"}, src.image.Digest, testBundleType)
	require.NoError(t, err)
	require.Len(t, referrers, 1)
	require.Equal(t, src.referrer.Digest, referrers[0].Digest)
	none, err := destination.Referrers(ctx, Artifact{Name: "team/app"}, digest.FromString("missing"), "")
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestSupplyChainTagNames(t *testing.T) {
	subject := digest.FromString("subject")
	require.Equal(t, "sha256-"+subject.Encoded()+".sig", supplyChainTag(subject, ".sig"))
	require.Equal(t, "sha256-"+subject.Encoded(), supplyChainTag(subject, ""))
	long := digest.SHA512.FromString("subject")
	require.Equal(t, "sha512-"+long.Encoded()[:64], supplyChainTag(long, ""))

	for _, tag := range []string{supplyChainTag(subject, ".sig"), supplyChainTag(subject, ".att"), supplyChainTag(subject, ".sbom"), supplyChainTag(subject, "")} {
		require.True(t, isSupplyChainTag(tag), tag)
	}
	for _, tag := range []string{"latest", "v1.2.3", "sha256-short.sig", "sha256-" + subject.Encoded() + ".txt"} {
		require.False(t, isSupplyChainTag(tag), tag)
	}
}
