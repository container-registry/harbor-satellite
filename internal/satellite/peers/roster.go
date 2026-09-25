// Package peers builds the allow-list a later copy step may ask.
// It does not dial peers or copy artifacts.
package peers

import (
	"net/url"
	"strings"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

// EligiblePeers merges static_peers with gc_peers and applies the group filter.
// A disabled block returns nil. The caller's slices are left unchanged.
func EligiblePeers(cfg config.PeerDistributionConfig) []config.PeerDescriptor {
	if !cfg.Enabled {
		return nil
	}

	merged := mergePeers(cfg.StaticPeers, cfg.GCPeers)
	return filterByReachout(merged, cfg.LocalGroups, cfg.ReachoutSats)
}

type rosterSlot struct {
	peer   config.PeerDescriptor
	static bool
}

func mergePeers(staticPeers, gcPeers []config.PeerDescriptor) []config.PeerDescriptor {
	slots := make([]rosterSlot, 0, len(staticPeers)+len(gcPeers))
	byID := make(map[string]int, len(staticPeers)+len(gcPeers))
	byURL := make(map[string]int, len(staticPeers)+len(gcPeers))

	for _, peer := range staticPeers {
		if peerIndex(byID, byURL, peer) >= 0 {
			continue
		}
		addPeer(&slots, byID, byURL, clonePeer(peer), true)
	}

	for _, peer := range gcPeers {
		index := peerIndex(byID, byURL, peer)
		if index < 0 {
			addPeer(&slots, byID, byURL, clonePeer(peer), false)
			continue
		}
		if slots[index].static && !sameGroupSet(slots[index].peer.Groups, peer.Groups) {
			slots[index].peer.Groups = cloneGroups(peer.Groups)
		}
	}

	out := make([]config.PeerDescriptor, len(slots))
	for i, slot := range slots {
		out[i] = slot.peer
	}
	return out
}

func addPeer(slots *[]rosterSlot, byID, byURL map[string]int, peer config.PeerDescriptor, static bool) {
	index := len(*slots)
	*slots = append(*slots, rosterSlot{peer: peer, static: static})
	if peer.ID != "" {
		byID[peer.ID] = index
	}
	if canonical, ok := canonicalPeerURL(string(peer.URL)); ok {
		byURL[canonical] = index
	}
}

// peerIndex matches a non-empty id first, then a canonical URL.
func peerIndex(byID, byURL map[string]int, peer config.PeerDescriptor) int {
	if peer.ID != "" {
		if index, ok := byID[peer.ID]; ok {
			return index
		}
	}
	canonical, ok := canonicalPeerURL(string(peer.URL))
	if !ok {
		return -1
	}
	if index, ok := byURL[canonical]; ok {
		return index
	}
	return -1
}

func canonicalPeerURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}

	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}

	path := strings.TrimRight(parsed.EscapedPath(), "/")
	return scheme + "://" + host + ":" + port + path, true
}

func filterByReachout(peers []config.PeerDescriptor, localGroups []string, reachout string) []config.PeerDescriptor {
	if reachout == "global" {
		if len(peers) == 0 {
			return nil
		}
		return peers
	}

	filtered := make([]config.PeerDescriptor, 0, len(peers))
	for _, peer := range peers {
		if groupsIntersect(localGroups, peer.Groups) {
			filtered = append(filtered, peer)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func groupsIntersect(localGroups, peerGroups []string) bool {
	if len(localGroups) == 0 || len(peerGroups) == 0 {
		return false
	}

	local := make(map[string]struct{}, len(localGroups))
	for _, group := range localGroups {
		if group == "" {
			continue
		}
		local[group] = struct{}{}
	}
	for _, group := range peerGroups {
		if group == "" {
			continue
		}
		if _, ok := local[group]; ok {
			return true
		}
	}
	return false
}

func sameGroupSet(left, right []string) bool {
	leftSet := groupSet(left)
	rightSet := groupSet(right)
	if len(leftSet) != len(rightSet) {
		return false
	}
	for group := range leftSet {
		if _, ok := rightSet[group]; !ok {
			return false
		}
	}
	return true
}

func groupSet(groups []string) map[string]struct{} {
	set := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		set[group] = struct{}{}
	}
	return set
}

func clonePeer(peer config.PeerDescriptor) config.PeerDescriptor {
	peer.Groups = cloneGroups(peer.Groups)
	return peer
}

func cloneGroups(groups []string) []string {
	if groups == nil {
		return nil
	}
	return append([]string(nil), groups...)
}
