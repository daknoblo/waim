package maintenance

import (
	"errors"
	"sync"
	"testing"
)

func TestExclusiveAdmissionAndEpoch(t *testing.T) {
	g := New()
	old := g.Token()
	done, err := g.Enter()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.TryReset(); !errors.Is(err, ErrBusy) {
		t.Fatal("reset admitted during work")
	}
	done()
	lease, err := g.TryReset()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.Enter(); !errors.Is(err, ErrBusy) {
				t.Error("work admitted during reset")
			}
			if _, err := g.TryReset(); !errors.Is(err, ErrBusy) {
				t.Error("concurrent reset admitted")
			}
		}()
	}
	wg.Wait()
	lease.Finish(1, 0, true)
	if _, err := g.Enter(); !errors.Is(err, ErrRecovery) {
		t.Fatal("partial reset admitted work")
	}
	lease, err = g.TryReset()
	if err != nil {
		t.Fatal(err)
	}
	lease.Finish(1, 1, false)
	if !errors.Is(g.Check(old), ErrStale) || !errors.Is(g.Check(""), ErrStale) {
		t.Fatal("stale page accepted")
	}
	if err := g.Check(g.Token()); err != nil {
		t.Fatal(err)
	}
}
