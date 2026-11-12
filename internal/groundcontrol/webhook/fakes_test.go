package webhook_test

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
)

var webhookNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func entry(xid uint64, id int64, kind fleet.EventKind) webhook.LogEntry {
	return webhook.LogEntry{
		Position: webhook.Position{XID: xid, ID: id}, Kind: kind,
		Aggregate: fleet.Aggregate{Type: fleet.AggregateGroup, ID: 3},
		Payload:   json.RawMessage(`{"name":"g1"}`), CreatedAt: webhookNow,
	}
}

func after(a, b webhook.Position) bool {
	return a.XID > b.XID || (a.XID == b.XID && a.ID > b.ID)
}

type logReader struct{ entries []webhook.LogEntry }

func (l *logReader) ReadAfter(_ context.Context, from webhook.Position, limit int) ([]webhook.LogEntry, error) {
	var out []webhook.LogEntry
	for _, e := range l.entries {
		if after(e.Position, from) && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

type failure struct {
	id                    fleet.WebhookID
	status                int
	retryIn, disableAfter time.Duration
}

type subStore struct {
	mu       sync.Mutex
	due      []webhook.DueSubscription
	advanced map[fleet.WebhookID]webhook.Position
	failures []failure
	disable  bool
}

func (s *subStore) Due(context.Context, int) ([]webhook.DueSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.due, nil
}

func (s *subStore) Advance(_ context.Context, id fleet.WebhookID, to webhook.Position, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.advanced == nil {
		s.advanced = map[fleet.WebhookID]webhook.Position{}
	}
	s.advanced[id] = to
	return nil
}

func (s *subStore) Fail(_ context.Context, id fleet.WebhookID, status int, _ string, retryIn, disableAfter time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = append(s.failures, failure{id: id, status: status, retryIn: retryIn, disableAfter: disableAfter})
	return s.disable, nil
}

type locker struct{ held map[fleet.WebhookID]bool }

func (l *locker) TryLock(_ context.Context, id fleet.WebhookID) (func(), bool, error) {
	if l.held[id] {
		return nil, false, nil
	}
	return func() {}, true, nil
}

// sender answers 500 for the event IDs in fail and 200 otherwise.
type sender struct {
	mu      sync.Mutex
	sent    []string
	secrets [][]byte
	fail    map[string]bool
	always  int
}

func (s *sender) Send(_ context.Context, _ string, secret []byte, event webhook.CloudEvent) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, event.ID)
	s.secrets = append(s.secrets, secret)
	if s.always != 0 {
		return s.always, nil
	}
	if s.fail[event.ID] {
		return 500, nil
	}
	return 200, nil
}

// sealer stores secrets as "sealed:" + plaintext.
type sealer struct{}

func (sealer) Seal(plaintext []byte) ([]byte, error) {
	return append([]byte("sealed:"), plaintext...), nil
}

func (sealer) Open(ciphertext []byte) ([]byte, error) {
	return ciphertext[len("sealed:"):], nil
}
