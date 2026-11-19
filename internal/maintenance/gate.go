// Package maintenance coordinates application work with exclusive destructive
// maintenance. Admission never waits: work arriving during a reset is rejected.
package maintenance

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrBusy = errors.New("data work or maintenance is in progress; retry when idle")
var ErrStale = errors.New("page predates a reset or restart; reload before saving")
var ErrRecovery = errors.New("factory reset recovery is pending; retry the reset or restart")

type Gate struct {
	mu                  sync.Mutex
	active              int
	exclusive, blocked  bool
	epoch, factoryEpoch int64
	instance            string
	resetAt             time.Time
}

func New() *Gate { return &Gate{instance: rand.Text()} }

// Enter admits a reader or worker. Release must cover its entire lifetime,
// including queued goroutine startup and final persistence.
func (g *Gate) Enter() (func(), error) {
	return g.EnterScheduled(time.Time{})
}

// EnterScheduled also discards a timer tick that became due before the last
// reset, including ticks whose goroutine was not scheduled during maintenance.
func (g *Gate) EnterScheduled(due time.Time) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocked {
		return nil, ErrRecovery
	}
	if g.exclusive {
		return nil, ErrBusy
	}
	if !due.IsZero() && due.Before(g.resetAt) {
		return nil, ErrStale
	}
	g.active++
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); g.active--; g.mu.Unlock() }) }, nil
}

type Lease struct {
	gate *Gate
	once sync.Once
}

func (g *Gate) TryReset() (*Lease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.exclusive || g.active != 0 {
		return nil, ErrBusy
	}
	g.exclusive = true
	return &Lease{gate: g}, nil
}

// Finish keeps the application closed to data work on an incomplete factory
// reset. A new exclusive lease can retry recovery; ordinary work cannot.
func (l *Lease) Finish(epoch, factoryEpoch int64, blocked bool) {
	l.once.Do(func() {
		g := l.gate
		g.mu.Lock()
		defer g.mu.Unlock()
		if epoch > g.epoch {
			g.resetAt = time.Now()
		}
		g.epoch = max(g.epoch, epoch)
		g.factoryEpoch = max(g.factoryEpoch, factoryEpoch)
		g.blocked, g.exclusive = blocked, false
	})
}

func (g *Gate) Epoch() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.epoch
}

func (g *Gate) FactoryEpoch() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.factoryEpoch
}

func (g *Gate) Token() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return fmt.Sprintf("%s:%d", g.instance, g.epoch)
}

func (g *Gate) Check(token string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Legacy clients may omit the token only before any reset has occurred.
	if token == "" && g.epoch == 0 {
		return nil
	}
	if token != fmt.Sprintf("%s:%d", g.instance, g.epoch) {
		return ErrStale
	}
	return nil
}
