package store

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
)

// PeerFillOptions controls the peer race for one artifact.
// Retries is the number of extra attempts after the first try.
// Concurrency is how many peers may copy that artifact at once.
type PeerFillOptions struct {
	Timeout     time.Duration
	Retries     int
	Concurrency int
}

// PeerFillingStore copies from peer stores before the Harbor source passed to
// Replicate. Each artifact is copied on its own. Eligible peers race, and the
// first complete graph cancels the others. Harbor is used only for an artifact
// no peer completed. Pull, Fetch, and Delete stay on the wrapped local store.
type PeerFillingStore struct {
	local Store
	peers func() []Store
	opts  PeerFillOptions

	// tagMu guards tagLocks. A registry tags inside Replicate, and each peer
	// race has its own slot channel, so two groups can publish one reference
	// at the same time unless the races share a lock.
	tagMu    sync.Mutex
	tagLocks map[string]*sync.Mutex
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
	if opts.Concurrency <= 0 {
		opts.Concurrency = config.DefaultPeerConcurrency
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
	unlock := s.lockCommittedTag(artifact.Reference())
	defer unlock()

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
	harborCtx := ctx
	if timeout := HarborTimeoutFrom(ctx); timeout > 0 {
		var cancel context.CancelFunc
		harborCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if copyErr := s.local.Replicate(harborCtx, source, one); copyErr != nil {
		return fmt.Errorf("copy %s from Harbor: %w", artifact.Reference(), copyErr)
	}
	return nil
}

type harborTimeoutKey struct{}

// WithHarborTimeout bounds the Harbor blob copy after peers miss. A live tick
// omits this so a large image can still finish. Degraded ticks set it so a
// dead registry cannot hang the scheduler.
func WithHarborTimeout(ctx context.Context, timeout time.Duration) context.Context {
	if timeout <= 0 {
		return ctx
	}
	return context.WithValue(ctx, harborTimeoutKey{}, timeout)
}

// HarborTimeoutFrom returns the Harbor blob timeout stored on ctx, or 0.
func HarborTimeoutFrom(ctx context.Context) time.Duration {
	timeout, ok := ctx.Value(harborTimeoutKey{}).(time.Duration)
	if !ok {
		return 0
	}
	return timeout
}

type peerCopyResult struct {
	peer Store
	err  error
}

func (s *PeerFillingStore) replicateFromPeers(ctx context.Context, artifacts []Artifact) (bool, error) {
	sources := s.peers()
	if len(sources) == 0 {
		return false, nil
	}

	// Cancel siblings only after a full Replicate succeeds. A peer miss is
	// expected and must leave the other copies running.
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan peerCopyResult, len(sources))
	var wg sync.WaitGroup
	var winOnce sync.Once
	onWin := func(peer Store) {
		winOnce.Do(func() {
			cancel()
			logger.FromContext(ctx).Info().Str("peer", storeLabel(peer)).Msg("artifact replicated from peer")
		})
	}
	// A registry tags inside Replicate. Canceling after the winner returns
	// cannot undo that PUT, so only one registry peer copy runs at a time.
	slots := make(chan struct{}, s.peerConcurrency())
	for _, peer := range sources {
		wg.Add(1)
		go func(peer Store) {
			defer wg.Done()
			select {
			case <-raceCtx.Done():
				results <- peerCopyResult{peer: peer, err: raceCtx.Err()}
				return
			case slots <- struct{}{}:
			}
			defer func() { <-slots }()
			// select can take the slot even when the race is already won.
			if err := raceCtx.Err(); err != nil {
				results <- peerCopyResult{peer: peer, err: err}
				return
			}
			results <- peerCopyResult{
				peer: peer,
				err:  replicateFromPeer(raceCtx, s.local, peer, artifacts, s.opts, func() { onWin(peer) }),
			}
		}(peer)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var failures []error
	won := false
	for result := range results {
		if result.err == nil {
			won = true
			continue
		}
		if won && errors.Is(result.err, context.Canceled) {
			continue
		}
		logger.FromContext(ctx).Warn().Err(result.err).Str("peer", storeLabel(result.peer)).Msg("peer copy failed")
		failures = append(failures, result.err)
	}
	if won {
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, errors.Join(failures...)
}

type peerRaceOnWinKey struct{}

// WithPeerRaceOnWin registers a callback the OCI store runs after a successful
// publish while it still holds the serving write lock. Peer races use it to
// cancel siblings before unlock so waiters on the publish lock do not publish.
func WithPeerRaceOnWin(ctx context.Context, onWin func()) context.Context {
	if onWin == nil {
		return ctx
	}
	return context.WithValue(ctx, peerRaceOnWinKey{}, onWin)
}

// peerConcurrency is how many peers may copy one artifact at once.
// OCI publish cancels siblings while it still holds the serving lock.
// A registry has no such lock, so its copies stay sequential.
func (s *PeerFillingStore) peerConcurrency() int {
	if destinationCommitsTags(s.local) {
		return 1
	}
	return s.opts.Concurrency
}

// lockCommittedTag lets one registry publish of a reference run at a time.
// OCI destinations return a no-op because the serving lock already covers them.
func (s *PeerFillingStore) lockCommittedTag(ref string) func() {
	if !destinationCommitsTags(s.local) {
		return func() {}
	}
	s.tagMu.Lock()
	if s.tagLocks == nil {
		s.tagLocks = make(map[string]*sync.Mutex)
	}
	lock := s.tagLocks[ref]
	if lock == nil {
		lock = &sync.Mutex{}
		s.tagLocks[ref] = lock
	}
	s.tagMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// tagCommitter marks a destination that commits a tag inside Replicate.
type tagCommitter interface {
	commitsTagsInReplicate()
}

func (*RegistryStore) commitsTagsInReplicate() {}

// destinationCommitsTags reports a store whose Replicate can move a tag before
// the caller is able to cancel the other publishers.
func destinationCommitsTags(local Store) bool {
	_, ok := local.(tagCommitter)
	return ok
}

func peerRaceOnWin(ctx context.Context) func() {
	fn, ok := ctx.Value(peerRaceOnWinKey{}).(func())
	if !ok {
		return nil
	}
	return fn
}

func replicateFromPeer(ctx context.Context, local, peer Store, artifacts []Artifact, opts PeerFillOptions, onWin func()) error {
	attempts := 1 + opts.Retries
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		if onWin != nil {
			attemptCtx = WithPeerRaceOnWin(attemptCtx, onWin)
		}
		last = local.Replicate(attemptCtx, peer, artifacts)
		cancel()
		if last == nil {
			if onWin != nil {
				onWin()
			}
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
