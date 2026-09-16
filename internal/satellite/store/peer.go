package store

import (
	"context"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

// PeerStore pulls artifacts from a static same-group replica-proxy allow-list
// when a digest is present, and falls back to the wrapped Harbor-backed store
// otherwise. Peers are probed concurrently; a miss or dead peer is skipped
// locally without waiting for Ground Control. Destination tags stay Harbor
// references (see OCIStore.CopyFrom).
type PeerStore struct {
	inner Store
	oci   *OCIStore
	peers []RegistryOptions
}

// NewPeerStore wraps oci with concurrent digest lookup against peers.
func NewPeerStore(oci *OCIStore, peers []RegistryOptions) *PeerStore {
	return &PeerStore{inner: oci, oci: oci, peers: peers}
}

// PeerOptionsFromURLs builds unauthenticated registry options for replica-proxy
// URLs. urls must be satellites in the same Ground Control group; out-of-group
// hosts must not be listed (no REACHOUT_SATS=global in this PoC).
func PeerOptionsFromURLs(urls []string, useUnsecure bool, tlsCfg config.TLSConfig) []RegistryOptions {
	peers := make([]RegistryOptions, 0, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		plainHTTP := useUnsecure || strings.HasPrefix(strings.ToLower(raw), "http://")
		peers = append(peers, RegistryOptions{
			Endpoint:  raw,
			PlainHTTP: plainHTTP,
			TLS:       tlsCfg,
		})
	}
	return peers
}

// Replicate copies each artifact from the first peer that has the digest
// (concurrent Resolve, first hit wins), or from Harbor when no peer has it
// or the peer copy fails.
func (s *PeerStore) Replicate(ctx context.Context, artifacts []Artifact) error {
	log := logger.FromContext(ctx)
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.replicateOne(ctx, artifact, log); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes artifacts from the local layout using the Harbor-tagged names.
func (s *PeerStore) Delete(ctx context.Context, artifacts []Artifact) error {
	return s.inner.Delete(ctx, artifacts)
}

func (s *PeerStore) replicateOne(ctx context.Context, artifact Artifact, log *zerolog.Logger) error {
	peer, ok := firstPeerWithDigest(ctx, s.peers, artifact)
	if !ok {
		log.Info().Str("reference", artifact.Reference()).Msg("No peer has artifact, falling back to Harbor")
		return s.inner.Replicate(ctx, []Artifact{artifact})
	}

	if err := s.oci.CopyFrom(ctx, peer, []Artifact{artifact}); err != nil {
		log.Warn().Err(err).Str("peer", peer.Endpoint).Str("reference", artifact.Reference()).
			Msg("Peer copy failed, falling back to Harbor")
		return s.inner.Replicate(ctx, []Artifact{artifact})
	}
	log.Info().
		Str("peer", peer.Endpoint).
		Str("reference", artifact.Reference()).
		Str("digest", artifact.sourceIdentifier()).
		Msg("Artifact copied from peer")
	return nil
}

// firstPeerWithDigest probes every peer concurrently with Resolve(digest).
// The first success wins and remaining probes are cancelled. Failures
// (not found, timeout, unreachable) are ignored so another peer or Harbor
// can still supply the artifact.
func firstPeerWithDigest(ctx context.Context, peers []RegistryOptions, artifact Artifact) (RegistryOptions, bool) {
	if len(peers) == 0 {
		return RegistryOptions{}, false
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	hits := make(chan RegistryOptions, 1)
	var wg sync.WaitGroup
	for _, peer := range peers {
		wg.Add(1)
		go func(peer RegistryOptions) {
			defer wg.Done()
			if err := probePeerDigest(ctx, peer, artifact); err != nil {
				return
			}
			select {
			case hits <- peer:
			case <-ctx.Done():
			}
		}(peer)
	}
	go func() {
		wg.Wait()
		close(hits)
	}()

	peer, ok := <-hits
	if !ok {
		return RegistryOptions{}, false
	}
	return peer, true
}

func probePeerDigest(ctx context.Context, peer RegistryOptions, artifact Artifact) error {
	repo, err := newRepository(peer, artifact)
	if err != nil {
		return err
	}
	_, err = repo.Resolve(ctx, artifact.sourceIdentifier())
	return err
}
