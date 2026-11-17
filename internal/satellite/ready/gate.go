// Package ready lets a manifest pull wait until state replication publishes
// that image into the local store.
package ready

import (
	"context"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
)

// Gate is the set of images the state job intends to publish. Until Publish,
// that set is unknown and manifest pulls wait instead of treating every image
// as outside it. A request for a published reference then blocks until that
// publish commits, the reference leaves the set, or the caller gives up.
type Gate struct {
	mu         sync.Mutex
	groups     map[string]map[string]struct{}
	refs       map[string]*refState
	blobs      map[string]map[string]struct{}
	imageBlobs map[string]map[string]map[string]struct{}
	blobUsers  map[string]int
	published  bool
	ready      chan struct{}
}

type refState struct {
	digest string
	ready  bool
	closed bool
	notify chan struct{}
}

// New returns an empty gate.
func New() *Gate {
	return &Gate{
		groups: make(map[string]map[string]struct{}),
		refs:   make(map[string]*refState),
		ready:  make(chan struct{}),
	}
}

// Publish marks the desired set as known. The first call releases manifest
// pulls that were waiting for it. Later calls do nothing.
func (g *Gate) Publish() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.published {
		return
	}
	g.published = true
	close(g.ready)
}

// WaitReady blocks until Publish or ctx ends.
func (g *Gate) WaitReady(ctx context.Context) error {
	g.mu.Lock()
	ready := g.ready
	published := g.published
	g.mu.Unlock()
	if published {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
		return nil
	}
}

// SetGroup replaces the references group will publish. A reference kept with
// the same digest stays in its current ready state. Any other change wakes
// waiters. A reference no other group still holds is dropped.
func (g *Gate) SetGroup(group string, artifacts []store.Artifact) {
	g.mu.Lock()
	defer g.mu.Unlock()

	next := make(map[string]string)
	for _, artifact := range artifacts {
		listed := listedDigest(artifact)
		for _, key := range referenceKeys(artifact) {
			next[key] = listed
		}
	}
	if g.groups == nil {
		g.groups = make(map[string]map[string]struct{})
	}
	if g.refs == nil {
		g.refs = make(map[string]*refState)
	}
	previous := g.groups[group]
	if len(next) == 0 {
		delete(g.groups, group)
		g.replaceBlobsLocked(group, "", nil)
	} else {
		keys := make(map[string]struct{}, len(next))
		for key, digest := range next {
			keys[key] = struct{}{}
			state, ok := g.refs[key]
			if ok && state.digest == digest {
				continue
			}
			if ok {
				state.close()
			}
			g.refs[key] = &refState{digest: digest, notify: make(chan struct{})}
		}
		g.groups[group] = keys
	}
	for key := range previous {
		if _, keep := next[key]; keep {
			continue
		}
		if g.heldByOtherGroup(group, key) {
			continue
		}
		if state, ok := g.refs[key]; ok {
			state.close()
			delete(g.refs, key)
		}
	}
}

// RemoveGroup drops every reference that only group was publishing.
func (g *Gate) RemoveGroup(group string) {
	g.SetGroup(group, nil)
}

// Commit marks artifact published and wakes waiters. A commit whose digest
// does not match the digest the group asked for is ignored.
func (g *Gate) Commit(artifact store.Artifact) {
	g.mu.Lock()
	defer g.mu.Unlock()
	listed := listedDigest(artifact)
	for _, key := range referenceKeys(artifact) {
		state, ok := g.refs[key]
		if !ok || state.ready {
			continue
		}
		if state.digest != "" && listed != "" && state.digest != listed {
			continue
		}
		state.ready = true
		if listed != "" {
			state.digest = listed
		}
		state.close()
	}
}

// SetBlobs replaces every successor digest recorded for group in repository.
// A pull for one of those digests stays local in that repository while the blob
// is absent. The same digest in another repository is not watched.
// An empty list clears the group. A digest another group still lists stays watched.
func (g *Gate) SetBlobs(group, repository string, digests []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.replaceBlobsLocked(group, repository, digests)
}

// SetImageBlobs replaces the successor digests recorded for one image in group.
// The other images in the group keep the digests already recorded for them.
func (g *Gate) SetImageBlobs(group, reference string, digests []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.setImageBlobsLocked(group, reference, digests)
}

// PruneImageBlobs drops images in group whose reference is not in keep.
func (g *Gate) PruneImageBlobs(group string, keep []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	images := g.imageBlobs[group]
	if len(images) == 0 {
		return
	}
	allowed := make(map[string]struct{}, len(keep))
	for _, reference := range keep {
		if reference == "" {
			continue
		}
		allowed[reference] = struct{}{}
	}
	for reference := range images {
		if _, ok := allowed[reference]; ok {
			continue
		}
		g.releaseImageBlobsLocked(group, reference)
	}
}

// WatchesBlob reports whether repository has digest in a desired image's graph.
// The same digest in another repository is not watched.
func (g *Gate) WatchesBlob(repository, digest string) bool {
	repository = strings.Trim(repository, "/")
	if repository == "" || digest == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.blobUsers[blobKey(repository, digest)] > 0
}

func (g *Gate) replaceBlobsLocked(group, repository string, digests []string) {
	g.clearImageBlobsLocked(group)
	if g.blobs == nil && len(digests) == 0 {
		return
	}
	if g.blobs == nil {
		g.blobs = make(map[string]map[string]struct{})
	}
	if g.blobUsers == nil {
		g.blobUsers = make(map[string]int)
	}
	for key := range g.blobs[group] {
		g.releaseBlobKeyLocked(key)
	}
	delete(g.blobs, group)
	repository = strings.Trim(repository, "/")
	if len(digests) == 0 || repository == "" {
		return
	}
	next := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		if digest == "" {
			continue
		}
		key := blobKey(repository, digest)
		if _, ok := next[key]; ok {
			continue
		}
		next[key] = struct{}{}
		g.blobUsers[key]++
	}
	if len(next) == 0 {
		return
	}
	g.blobs[group] = next
}

func (g *Gate) setImageBlobsLocked(group, reference string, digests []string) {
	if group == "" || reference == "" {
		return
	}
	if g.blobUsers == nil {
		g.blobUsers = make(map[string]int)
	}
	g.releaseImageBlobsLocked(group, reference)
	repository := repositoryFromReference(reference)
	if repository == "" {
		return
	}
	next := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		if digest == "" {
			continue
		}
		if _, ok := next[digest]; ok {
			continue
		}
		next[digest] = struct{}{}
		g.blobUsers[blobKey(repository, digest)]++
	}
	if len(next) == 0 {
		return
	}
	if g.imageBlobs == nil {
		g.imageBlobs = make(map[string]map[string]map[string]struct{})
	}
	if g.imageBlobs[group] == nil {
		g.imageBlobs[group] = make(map[string]map[string]struct{})
	}
	g.imageBlobs[group][reference] = next
}

func (g *Gate) clearImageBlobsLocked(group string) {
	for reference := range g.imageBlobs[group] {
		g.releaseImageBlobsLocked(group, reference)
	}
}

func (g *Gate) releaseImageBlobsLocked(group, reference string) {
	images := g.imageBlobs[group]
	if len(images) == 0 {
		return
	}
	repository := repositoryFromReference(reference)
	for digest := range images[reference] {
		g.releaseBlobKeyLocked(blobKey(repository, digest))
	}
	delete(images, reference)
	if len(images) == 0 {
		delete(g.imageBlobs, group)
	}
}

func (g *Gate) releaseBlobKeyLocked(key string) {
	if g.blobUsers == nil {
		return
	}
	g.blobUsers[key]--
	if g.blobUsers[key] <= 0 {
		delete(g.blobUsers, key)
	}
}

func blobKey(repository, digest string) string {
	return repository + "\x00" + digest
}

func repositoryFromReference(reference string) string {
	repository := reference
	if name, _, found := strings.Cut(reference, "@"); found {
		repository = name
	} else if name, _, found := strings.Cut(reference, ":"); found {
		repository = name
	}
	return strings.Trim(repository, "/")
}

// Known reports whether artifact is in the current set, committed or not.
func (g *Gate) Known(artifact store.Artifact) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.refs[artifact.Reference()]
	return ok
}

// Watching reports whether artifact is in the current set and not yet committed.
func (g *Gate) Watching(artifact store.Artifact) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.refs[artifact.Reference()]
	return ok && !state.ready
}

// ExpectedDigest returns the digest the state job will publish for artifact.
func (g *Gate) ExpectedDigest(artifact store.Artifact) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.refs[artifact.Reference()]
	if !ok || state.digest == "" {
		return "", false
	}
	return state.digest, true
}

// Wait blocks until artifact is committed, dropped from the set, or ctx ends.
// The boolean is true when the commit happened.
func (g *Gate) Wait(ctx context.Context, artifact store.Artifact) (bool, error) {
	for {
		g.mu.Lock()
		state, ok := g.refs[artifact.Reference()]
		if !ok {
			g.mu.Unlock()
			return false, nil
		}
		if state.ready {
			g.mu.Unlock()
			return true, nil
		}
		notify := state.notify
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-notify:
		}
	}
}

func (g *Gate) heldByOtherGroup(except, key string) bool {
	for name, keys := range g.groups {
		if name == except {
			continue
		}
		if _, ok := keys[key]; ok {
			return true
		}
	}
	return false
}

func (s *refState) close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	close(s.notify)
}

func listedDigest(artifact store.Artifact) string {
	digest := artifact.Digest
	if digest == "" {
		return ""
	}
	if _, after, found := strings.Cut(digest, "@"); found {
		return after
	}
	return digest
}

func referenceKeys(artifact store.Artifact) []string {
	var keys []string
	seen := make(map[string]struct{})
	add := func(key string) {
		if key == "" || strings.HasSuffix(key, ":") || strings.HasSuffix(key, "@") {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if artifact.Tag != "" || artifact.Digest != "" {
		add(artifact.Reference())
	}
	if artifact.Digest != "" && artifact.Tag != "" {
		digestForm := artifact
		digestForm.Tag = ""
		add(digestForm.Reference())
	}
	return keys
}
