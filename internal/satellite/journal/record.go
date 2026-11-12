package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
)

// Record is one journal entry. Append assigns Seq; Payload is the
// edgejournal payload of Type.
type Record struct {
	Seq        int64                  `json:"seq"`
	Type       edgejournal.RecordType `json:"type"`
	RecordedAt time.Time              `json:"recorded_at"`
	Payload    json.RawMessage        `json:"payload"`
}

// RecordPullThrough queues a pull-through record without blocking; under
// backpressure it is counted as dropped.
func (j *Journal) RecordPullThrough(p edgejournal.PullThrough) {
	j.tryAppendPayload(edgejournal.RecordPullThrough, p)
}

// AppendCRI journals the result of configuring one container runtime.
func (j *Journal) AppendCRI(ctx context.Context, c edgejournal.CRI) error {
	payload, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode CRI record: %w", err)
	}
	_, err = j.Append(ctx, Record{Type: edgejournal.RecordCRI, RecordedAt: time.Now().UTC(), Payload: payload})
	return err
}

func (j *Journal) tryAppendPayload(t edgejournal.RecordType, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	j.TryAppend(Record{Type: t, RecordedAt: time.Now().UTC(), Payload: payload})
}
