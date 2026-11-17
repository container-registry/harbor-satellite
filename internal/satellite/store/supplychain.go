package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"
)

const (
	// maxReferrerDepth bounds referrers of referrers, such as a signature on
	// an SBOM that is itself attached to the image.
	maxReferrerDepth = 4
	// maxSupplyChainManifests bounds the manifests copied for one root, so a
	// subject with a runaway referrer list cannot stall replication.
	maxSupplyChainManifests = 512
	// mediaTypeDockerManifest and mediaTypeDockerManifestList are the Docker
	// schema 2 types Harbor still serves for images pushed by Docker.
	mediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	mediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// cosignTagSuffixes are the tags cosign writes next to a signed digest when
// it uses the tag scheme: sha256-<hex>.sig, .att and .sbom.
func cosignTagSuffixes() []string {
	return []string{".sig", ".att", ".sbom"}
}

// supplyChainTag returns the tag-scheme name for subject. An empty suffix is
// the OCI 1.1 referrers tag schema fallback, <alg>-<ref> with ref truncated
// to 64 characters.
//
// Refer: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#referrers-tag-schema
func supplyChainTag(subject digest.Digest, suffix string) string {
	alg := subject.Algorithm().String()
	if len(alg) > 32 {
		alg = alg[:32]
	}
	ref := subject.Encoded()
	if len(ref) > 64 {
		ref = ref[:64]
	}
	return alg + "-" + ref + suffix
}

// isSupplyChainTag reports whether tag is a cosign tag or a referrers tag
// schema name, both derived from a subject digest.
func isSupplyChainTag(tag string) bool {
	for _, suffix := range cosignTagSuffixes() {
		tag = strings.TrimSuffix(tag, suffix)
	}
	alg, encoded, found := strings.Cut(tag, "-")
	if !found {
		return false
	}
	return digest.NewDigestFromEncoded(digest.Algorithm(alg), encoded).Validate() == nil
}

func isManifestMediaType(mediaType string) bool {
	switch mediaType {
	case ocispec.MediaTypeImageManifest, ocispec.MediaTypeImageIndex,
		mediaTypeDockerManifest, mediaTypeDockerManifestList:
		return true
	default:
		return false
	}
}

// taggedDescriptor is a manifest the local store tags next to a copied root.
type taggedDescriptor struct {
	tag  string
	desc ocispec.Descriptor
}

// subjectReferrers lists the OCI 1.1 referrers of one subject manifest.
type subjectReferrers struct {
	subject   digest.Digest
	referrers []ocispec.Descriptor
}

// supplyChain holds the signatures, attestations and SBOMs that describe one
// copied root and the manifests inside it.
type supplyChain struct {
	// tagged are cosign tag-scheme manifests, in discovery order.
	tagged []taggedDescriptor
	// referrers are grouped by subject, parents before their own referrers,
	// so a copy never pushes a manifest before its subject.
	referrers []subjectReferrers
}

func (c supplyChain) empty() bool {
	return len(c.tagged) == 0 && len(c.referrers) == 0
}

func (c supplyChain) manifestCount() int {
	count := len(c.tagged)
	for _, group := range c.referrers {
		count += len(group.referrers)
	}
	return count
}

// discoverSupplyChain finds the cosign tags and OCI referrers in src for root
// and every manifest root links to, such as the platform manifests of an
// index that cosign signs one by one. graph is any store that already holds
// the root graph. A source without a referrers API is read through the tag
// schema fallback by ORAS.
//
// A failed lookup is logged and skipped. Signatures are optional content: a
// proxy-mode peer asked for a missing .sig tag while Harbor is down answers
// with an error, and that must not stop the image copy. A signature policy
// still rejects the image when its signature was not found.
func discoverSupplyChain(ctx context.Context, src oras.ReadOnlyTarget, graph content.Fetcher, root ocispec.Descriptor) (supplyChain, error) {
	subjects, err := manifestsIn(ctx, graph, root)
	if err != nil {
		return supplyChain{}, fmt.Errorf("list signed manifests of %s: %w", root.Digest, err)
	}
	var chain supplyChain
	if chain.tagged, err = discoverCosignTags(ctx, src, subjects); err != nil {
		return supplyChain{}, fmt.Errorf("%s: %w", root.Digest, err)
	}
	lister, ok := src.(content.ReadOnlyGraphStorage)
	if !ok {
		return chain, nil
	}
	if chain.referrers, err = discoverReferrers(ctx, lister, subjects, len(chain.tagged)); err != nil {
		return supplyChain{}, fmt.Errorf("%s: %w", root.Digest, err)
	}
	return chain, nil
}

// manifestsIn returns root and every manifest or index reachable from it.
func manifestsIn(ctx context.Context, graph content.Fetcher, root ocispec.Descriptor) ([]ocispec.Descriptor, error) {
	var manifests []ocispec.Descriptor
	err := walkDescriptorGraph(ctx, graph, root, func(desc ocispec.Descriptor) error {
		if isManifestMediaType(desc.MediaType) {
			manifests = append(manifests, desc)
		}
		return nil
	})
	return manifests, err
}

// discoverCosignTags resolves the cosign tag-scheme names of each subject.
func discoverCosignTags(ctx context.Context, src oras.ReadOnlyTarget, subjects []ocispec.Descriptor) ([]taggedDescriptor, error) {
	var tagged []taggedDescriptor
	for _, subject := range subjects {
		for _, suffix := range cosignTagSuffixes() {
			tag := supplyChainTag(subject.Digest, suffix)
			desc, err := src.Resolve(ctx, tag)
			if errors.Is(err, errdef.ErrNotFound) {
				continue
			}
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				logger.FromContext(ctx).Warn().Err(err).Str("tag", tag).Msg("skipping signature tag that could not be resolved")
				continue
			}
			tagged = append(tagged, taggedDescriptor{tag: tag, desc: desc})
			if len(tagged) > maxSupplyChainManifests {
				return nil, fmt.Errorf("more than %d signature and referrer manifests", maxSupplyChainManifests)
			}
		}
	}
	return tagged, nil
}

// discoverReferrers lists referrers level by level, parents before their own
// referrers, up to maxReferrerDepth. counted is the number of manifests the
// caller already holds toward maxSupplyChainManifests.
func discoverReferrers(ctx context.Context, lister content.ReadOnlyGraphStorage, subjects []ocispec.Descriptor, counted int) ([]subjectReferrers, error) {
	var groups []subjectReferrers
	seen := make(map[digest.Digest]struct{})
	level := subjects
	for depth := 0; depth < maxReferrerDepth && len(level) > 0; depth++ {
		var next []ocispec.Descriptor
		for _, subject := range level {
			referrers, err := registry.Referrers(ctx, lister, subject, "")
			if err != nil && !errors.Is(err, errdef.ErrNotFound) {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				logger.FromContext(ctx).Warn().Err(err).Str("subject", subject.Digest.String()).Msg("skipping referrers that could not be listed")
				continue
			}
			fresh := unseen(referrers, seen)
			if len(fresh) == 0 {
				continue
			}
			groups = append(groups, subjectReferrers{subject: subject.Digest, referrers: fresh})
			next = append(next, fresh...)
			counted += len(fresh)
			if counted > maxSupplyChainManifests {
				return nil, fmt.Errorf("more than %d signature and referrer manifests", maxSupplyChainManifests)
			}
		}
		level = next
	}
	return groups, nil
}

// unseen returns the descriptors not yet in seen and records them there.
func unseen(descs []ocispec.Descriptor, seen map[digest.Digest]struct{}) []ocispec.Descriptor {
	var fresh []ocispec.Descriptor
	for _, desc := range descs {
		if _, dup := seen[desc.Digest]; dup {
			continue
		}
		seen[desc.Digest] = struct{}{}
		fresh = append(fresh, desc)
	}
	return fresh
}

// copyTo copies every supply-chain manifest graph from src to dst. Subjects
// are copied first, so dst already holds each manifest's subject.
func (c supplyChain) copyTo(ctx context.Context, src content.ReadOnlyStorage, dst content.Storage) error {
	for _, tagged := range c.tagged {
		if err := oras.CopyGraph(ctx, src, dst, tagged.desc, oras.DefaultCopyGraphOptions); err != nil {
			return fmt.Errorf("copy %s: %w", tagged.tag, err)
		}
	}
	for _, group := range c.referrers {
		for _, referrer := range group.referrers {
			if err := oras.CopyGraph(ctx, src, dst, referrer, oras.DefaultCopyGraphOptions); err != nil {
				return fmt.Errorf("copy referrer %s of %s: %w", referrer.Digest, group.subject, err)
			}
		}
	}
	return nil
}

// referrersIndex builds the image index the OCI referrers tag schema stores
// under supplyChainTag(subject, ""). The local store keeps referrers this way:
// the tag holds them through garbage collection and serves the referrers API.
func referrersIndex(referrers []ocispec.Descriptor) (ocispec.Descriptor, []byte, error) {
	manifests := make([]ocispec.Descriptor, len(referrers))
	copy(manifests, referrers)
	payload, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	})
	if err != nil {
		return ocispec.Descriptor{}, nil, fmt.Errorf("encode referrers index: %w", err)
	}
	return content.NewDescriptorFromBytes(ocispec.MediaTypeImageIndex, payload), payload, nil
}

// filterArtifactType keeps referrers of artifactType. An empty type keeps all.
func filterArtifactType(referrers []ocispec.Descriptor, artifactType string) []ocispec.Descriptor {
	if artifactType == "" {
		return referrers
	}
	var filtered []ocispec.Descriptor
	for _, referrer := range referrers {
		if referrer.ArtifactType == artifactType {
			filtered = append(filtered, referrer)
		}
	}
	return filtered
}
