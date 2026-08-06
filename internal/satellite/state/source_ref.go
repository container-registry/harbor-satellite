package state

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// sourceRepository builds the fully qualified repository path for an entity in
// the source registry, without any tag or digest suffix.
func sourceRepository(srcRegistry string, entity Entity) string {
	return fmt.Sprintf("%s/%s/%s", srcRegistry, entity.GetRepository(), entity.GetName())
}

// sourceTagRef builds the tag reference for an entity in the source registry.
// It names the image but does not identify its content: tags are mutable and
// must never be used to pull. Use pinnedSourceRef for that.
func sourceTagRef(srcRegistry string, entity Entity, nameOpts []name.Option) (name.Reference, error) {
	ref := fmt.Sprintf("%s:%s", sourceRepository(srcRegistry, entity), entity.GetTag())
	parsed, err := name.ParseReference(ref, nameOpts...)
	if err != nil {
		return nil, fmt.Errorf("parse source ref %s: %w", ref, err)
	}
	return parsed, nil
}

// pinnedSourceRef returns a digest-pinned reference to the entity's image in the
// source registry.
//
// A tag can be repointed at different content at any moment, including between
// the instant Ground Control computes the desired state and the instant the
// satellite pulls. Pulling by digest makes the registry protocol itself
// guarantee content identity: go-containerregistry verifies the manifest it
// receives hashes to the requested digest and rejects it otherwise.
//
// When the desired state carries a digest, that digest is used. When it does
// not, the tag is resolved to a digest exactly once here, and every subsequent
// operation runs against the resolved digest, so a tag moved mid-flight cannot
// substitute different content.
func pinnedSourceRef(srcRegistry string, entity Entity, nameOpts []name.Option, pullOpts []remote.Option) (name.Digest, error) {
	repo := sourceRepository(srcRegistry, entity)

	if digest := strings.TrimSpace(entity.Digest); digest != "" {
		ref, err := name.NewDigest(fmt.Sprintf("%s@%s", repo, digest), nameOpts...)
		if err != nil {
			return name.Digest{}, fmt.Errorf("parse digest ref %s@%s: %w", repo, digest, err)
		}
		return ref, nil
	}

	tagRef, err := sourceTagRef(srcRegistry, entity, nameOpts)
	if err != nil {
		return name.Digest{}, err
	}

	desc, err := remote.Head(tagRef, pullOpts...)
	if err != nil {
		return name.Digest{}, fmt.Errorf("resolve tag %s to digest: %w", tagRef, err)
	}

	ref, err := name.NewDigest(fmt.Sprintf("%s@%s", repo, desc.Digest.String()), nameOpts...)
	if err != nil {
		return name.Digest{}, fmt.Errorf("parse resolved digest ref %s@%s: %w", repo, desc.Digest, err)
	}
	return ref, nil
}

// verifyFetchedDigest fails loudly when the manifest served for a digest-pinned
// reference does not hash to the digest that was requested. go-containerregistry
// already enforces this, so a mismatch here means the guarantee was bypassed;
// checking is cheap and the alternative is silently publishing substituted
// content to the local registry.
func verifyFetchedDigest(want name.Digest, got string) error {
	if want.DigestStr() != got {
		return fmt.Errorf("digest mismatch for %s: manifest hashes to %s, desired state requires %s",
			want.Context().Name(), got, want.DigestStr())
	}
	return nil
}
