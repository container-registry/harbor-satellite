package journal

import (
	"encoding/json"
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
