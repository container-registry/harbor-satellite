package store

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
)

// PeerFillOptions controls how long and how often one peer is tried before the next.
// Retries is the number of extra attempts after the first try.
type PeerFillOptions struct {
	Timeout time.Duration
	Retries int
}

// PeerFillingStore copies from peer stores before the Harbor source passed to
// Replicate. Each artifact is copied on its own, and peers are tried one at a
// time for that artifact. Harbor is used only for an artifact no peer
// completed. Pull, Fetch, and Delete stay on the wrapped local store.
type PeerFillingStore struct {
	local Store
	peers func() []Store
	opts  PeerFillOptions
}

var _ Store = (*PeerFillingStore)(nil)

// NewPeerFillingStore wraps local and tries peers on every Replicate call.
func NewPeerFillingStore(local Store, peers func() []Store, opts PeerFillOptions) (*PeerFillingStore, error) {
	if local == nil {
		return nil, errors.New("peer filling store requires a local store")
	}
	if peers == nil {
		peers = func() []Store { return nil }
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}
	return &PeerFillingStore{local: local, peers: peers, opts: opts}, nil
}

func (s *PeerFillingStore) Pull(ctx context.Context, artifact Artifact, resource PullResource) (ocispec.Descriptor, error) {
	return s.local.Pull(ctx, artifact, resource)
}

func (s *PeerFillingStore) Fetch(ctx context.Context, artifact Artifact, descriptor ocispec.Descriptor, resource PullResource, headers http.Header) (*http.Response, error) {
	return s.local.Fetch(ctx, artifact, descriptor, resource, headers)
}

func (s *PeerFillingStore) Delete(ctx context.Context, artifacts []Artifact) error {
	return s.local.Delete(ctx, artifacts)
}

func (s *PeerFillingStore) Replicate(ctx context.Context, source Store, artifacts []Artifact) error {
	var failed []error
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.replicateArtifact(ctx, source, artifact); err != nil {
			if ctx.Err() != nil {
				return err
			}
			failed = append(failed, err)
		}
	}
	return errors.Join(failed...)
}

func (s *PeerFillingStore) replicateArtifact(ctx context.Context, source Store, artifact Artifact) error {
	one := []Artifact{artifact}
	copied, err := s.replicateFromPeers(ctx, one)
	if copied {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		logger.FromContext(ctx).Warn().Err(err).Str("reference", artifact.Reference()).Msg("peer copy did not complete a graph; trying Harbor")
	}
	if source == nil {
		if err != nil {
			return fmt.Errorf("no complete peer graph for %s and no Harbor source: %w", artifact.Reference(), err)
		}
		return fmt.Errorf("no complete peer graph for %s and no Harbor source", artifact.Reference())
	}
	if err != nil {
		logger.FromContext(ctx).Info().Str("reference", artifact.Reference()).Msg("filling local store from Harbor")
	}
	if copyErr := s.local.Replicate(ctx, source, one); copyErr != nil {
		return fmt.Errorf("copy %s from Harbor: %w", artifact.Reference(), copyErr)
	}
	return nil
}

func (s *PeerFillingStore) replicateFromPeers(ctx context.Context, artifacts []Artifact) (bool, error) {
	sources := s.peers()
	if len(sources) == 0 {
		return false, nil
	}

	var last error
	for _, peer := range sources {
		last = replicateFromPeer(ctx, s.local, peer, artifacts, s.opts)
		if last == nil {
			logger.FromContext(ctx).Info().Str("peer", storeLabel(peer)).Msg("artifact replicated from peer")
			return true, nil
		}
		logger.FromContext(ctx).Warn().Err(last).Str("peer", storeLabel(peer)).Msg("peer copy failed")
	}
	return false, last
}

func replicateFromPeer(ctx context.Context, local, peer Store, artifacts []Artifact, opts PeerFillOptions) error {
	attempts := 1 + opts.Retries
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		last = local.Replicate(attemptCtx, peer, artifacts)
		cancel()
		if last == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return last
}

type labeledStore struct {
	Store
	label string
}

func (s labeledStore) Label() string { return s.label }

// targetFor keeps the log label from hiding the inner registry. OCI copy
// accepts a source only when it can open that source's ORAS target.
func (s labeledStore) targetFor(artifact Artifact) (oras.Target, error) {
	target, ok := s.Store.(storeTarget)
	if !ok {
		return nil, errors.New("source store does not support OCI graph transfer")
	}
	return target.targetFor(artifact)
}

func storeLabel(source Store) string {
	labeled, ok := source.(interface{ Label() string })
	if ok && labeled.Label() != "" {
		return labeled.Label()
	}
	return "peer"
}

// LabelStore annotates a Store so replicate logs can name the peer URL.
func LabelStore(inner Store, label string) Store {
	if inner == nil {
		return nil
	}
	return labeledStore{Store: inner, label: label}
}
