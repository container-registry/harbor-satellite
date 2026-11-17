package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// ErrSignatureRejected marks an artifact the signature policy keeps out of
// the store. Replicate skips that artifact and still copies the others.
var ErrSignatureRejected = errors.New("signature policy rejected the artifact")

// Signatures are the signature candidates of one subject manifest, read
// through Fetcher.
type Signatures struct {
	Fetcher content.Fetcher
	// Subject is the manifest the signatures must cover.
	Subject ocispec.Descriptor
	// Tagged are cosign tag-scheme .sig manifests of Subject.
	Tagged []ocispec.Descriptor
	// Referrers are the OCI referrers of Subject.
	Referrers []ocispec.Descriptor
}

// SignatureVerifier decides whether a copied root may be published. Verify
// returns an error wrapping ErrSignatureRejected when the root must stay out
// of the store, and nil when it passes or the policy only warns.
type SignatureVerifier interface {
	Verify(ctx context.Context, reference string, signatures Signatures) error
}

// signaturesOf returns the signature candidates of root from chain.
func (c supplyChain) signaturesOf(fetcher content.Fetcher, root ocispec.Descriptor) Signatures {
	signatures := Signatures{Fetcher: fetcher, Subject: root}
	sigTag := supplyChainTag(root.Digest, ".sig")
	for _, tagged := range c.tagged {
		if tagged.tag == sigTag {
			signatures.Tagged = append(signatures.Tagged, tagged.desc)
		}
	}
	for _, group := range c.referrers {
		if group.subject == root.Digest {
			signatures.Referrers = append(signatures.Referrers, group.referrers...)
		}
	}
	return signatures
}

// verifySignatures applies verifier to a copied root. Signatures, SBOMs and
// other referrer artifacts are not images and are never checked themselves:
// a cosign tag is skipped by name, and a manifest with a subject is skipped
// unless its config is an image config a runtime could start.
func verifySignatures(ctx context.Context, verifier SignatureVerifier, artifact Artifact, chain supplyChain, fetcher content.Fetcher, root ocispec.Descriptor) error {
	if verifier == nil || artifact.Tag == "" || isSupplyChainTag(artifact.Tag) {
		return nil
	}
	referrer, err := isReferrerArtifact(ctx, fetcher, root)
	if err != nil {
		return err
	}
	if referrer {
		return nil
	}
	return verifier.Verify(ctx, artifact.Reference(), chain.signaturesOf(fetcher, root))
}

func isReferrerArtifact(ctx context.Context, fetcher content.Fetcher, desc ocispec.Descriptor) (bool, error) {
	if desc.MediaType != ocispec.MediaTypeImageManifest {
		return false, nil
	}
	payload, err := content.FetchAll(ctx, fetcher, desc)
	if err != nil {
		return false, fmt.Errorf("read manifest %s: %w", desc.Digest, err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return false, fmt.Errorf("decode manifest %s: %w", desc.Digest, err)
	}
	if manifest.Subject == nil {
		return false, nil
	}
	return manifest.Config.MediaType != ocispec.MediaTypeImageConfig &&
		manifest.Config.MediaType != mediaTypeDockerImageConfig, nil
}
