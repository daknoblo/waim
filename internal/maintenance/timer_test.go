package maintenance

import (
	"errors"
	"testing"
	"time"
)

func TestOldTimerTickIsDiscardedAfterReset(t *testing.T) {
	g := New()
	due := time.Now().Add(-time.Second)
	lease, err := g.TryReset()
	if err != nil {
		t.Fatal(err)
	}
	lease.Finish(1, 0, false)
	if _, err := g.EnterScheduled(due); !errors.Is(err, ErrStale) {
		t.Fatal("old timer tick admitted after reset")
	}
	release, err := g.EnterScheduled(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal("future scheduled work incorrectly blocked")
	}
	release()
}
