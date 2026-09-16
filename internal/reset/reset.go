// Package reset coordinates transactional data resets and recoverable factory
// resets without deleting the database file or the persistent encryption key.
package reset

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/store"
)

type Service struct {
	cfg   *config.Manager
	store *store.Store
}

func New(cfg *config.Manager, st *store.Store) *Service { return &Service{cfg, st} }

// Recover must run before workers/HTTP startup. A committed factory reset with
// incomplete configuration persistence is completed idempotently, or startup
// fails closed. Its database deletion is never repeated.
func (s *Service) Recover(ctx context.Context) error {
	return s.exclusive(ctx, "", nil)
}

// Apply rejects busy applications immediately. after runs under the same lease
// after persistence succeeds, to invalidate in-memory and queued work.
func (s *Service) Apply(ctx context.Context, scope store.ResetScope, token string, after func()) error {
	if !scope.Valid() {
		return errors.New("unknown reset scope")
	}
	return s.exclusive(ctx, scope, after, token)
}

func (s *Service) exclusive(ctx context.Context, scope store.ResetScope, after func(), tokens ...string) error {
	lease, err := s.cfg.Gate().TryReset()
	if err != nil {
		return err
	}
	state := store.ResetState{Epoch: s.cfg.Gate().Epoch(), FactoryEpoch: s.cfg.Gate().FactoryEpoch()}
	blocked := false
	defer func() { lease.Finish(state.Epoch, state.FactoryEpoch, blocked) }()
	// Do not abandon half of a committed factory reset when the browser leaves.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	state, err = s.store.ResetState(ctx)
	if err != nil {
		blocked = true
		return err
	}
	if len(tokens) > 0 && !state.FactoryPending {
		if tokens[0] == "" {
			return errors.New("reset token required")
		}
		if err := s.cfg.Gate().Check(tokens[0]); err != nil {
			return err
		}
	}
	if state.FactoryPending && scope != "" && scope != store.ResetFactory {
		blocked = true
		return errors.New("factory reset recovery required; retry factory reset or restart")
	}
	if !state.FactoryPending && scope != "" {
		next, err := s.store.ResetData(ctx, scope)
		if err != nil {
			observed, readErr := s.store.ResetState(ctx)
			blocked = readErr != nil || observed.FactoryPending || observed.Epoch != state.Epoch
			if readErr == nil {
				state = observed
			}
			return err
		}
		state = next
	}
	if state.FactoryPending {
		blocked = true
		if err := s.cfg.FactoryDefaults(); err != nil {
			return fmt.Errorf("factory reset pending: configuration could not be reset; retry or restart: %w", err)
		}
		if err := s.store.FinishFactoryReset(ctx); err != nil {
			return fmt.Errorf("factory reset pending: completion could not be recorded: %w", err)
		}
		blocked = false
	}
	if after != nil {
		after()
	}
	return nil
}
