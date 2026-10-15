// Package pending marks tests for Harbor 3 plan items (defects D1..D9 and
// planned features) whose implementation has not landed yet. They are
// skipped until the implementing change removes the marker; set
// HARBOR3_PENDING=1 to run them and watch them fail.
package pending

import (
	"os"
	"testing"
)

func Plan(t testing.TB, item, summary string) {
	t.Helper()
	if os.Getenv("HARBOR3_PENDING") == "" {
		t.Skipf("pending plan item %s: %s", item, summary)
	}
}
